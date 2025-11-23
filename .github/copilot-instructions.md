# Grafoo - GitHub Copilot Instructions

## Project Overview

**Grafoo** is a Kubernetes operator for OpenShift that deploys and manages Grafana instances with integrated observability datasources (Prometheus, Loki, Tempo). Built with Kubebuilder v4, it automatically configures datasources, authentication via Dex OIDC, and MariaDB persistence.

**Key Components:**
- **Main Operator** (`cmd/grafoo`): Reconciles `Grafana` CRs, managing Grafana instances, Dex, MariaDB, and datasources
- **DSProxy** (`cmd/dsproxy`): Datasource proxy with iptables-based traffic interception, JWT authentication, and OPA authorization
- **Custom Resource**: `grafoo.cloudmonkey.org/v1alpha1/Grafana` with webhook-based defaulting and validation

## Architecture

### Component Reconciliation Flow
The controller reconciles in this order (see `internal/controller/controller.go`):
1. **Token Management**: Check if service account tokens need refresh based on `TokenExpirationTime`
2. **Dex** (`ReconcileDex`): Deploy Dex OIDC provider with OpenShift OAuth integration
3. **MariaDB** (`ReconcileMariaDB`): Deploy MariaDB for Grafana persistence
4. **Grafana** (`ReconcileGrafana`): Deploy Grafana via grafana-operator v5 CRs
5. **DataSources** (`ReconcileDataSources`): Create GrafanaDatasource CRs with service account tokens

Each sub-reconciler is modular (`dex.go`, `mariadb.go`, `grafana.go`, `datasource.go`) and updates specific status conditions (`DexReady`, `MariaDBReady`, etc.).

### Token Lifecycle Pattern
- Service account tokens are **bound tokens** with configurable expiration (`TokenDuration`, default 24h)
- Created via `ServiceAccounts(...).CreateToken()` API, not static secrets
- Stored in Dex config and datasource `SecureJSONData` as bearer tokens
- Controller requeues 5min before expiration to refresh tokens across all components
- Token refresh triggers cascading updates: Dex config secret → deployment restart, datasources update

### OpenShift Integration
- Uses `config.openshift.io/v1/Ingress` to discover cluster domain for routes
- Dex connects to OpenShift OAuth via service account with `oauth-redirecturi` annotation
- RBAC permissions for Loki/Tempo/Prometheus are granted via `SubjectAccessReview` checks

## Development Workflow

### Building & Running
```bash
# Local development (run controller outside cluster)
make run

# Build container images (uses ko, not docker)
make docker-build  # Builds multi-arch (amd64/arm64) via ko

# Deploy to cluster
make install       # Install CRDs
make deploy        # Deploy operator

# Run tests
make test          # Unit tests with envtest
make test-e2e      # E2E tests (requires cluster)
```

**Note**: `docker-build` uses `ko` (not Docker) and builds from `cmd/grafoo`. Set `KO_DOCKER_REPO` to override image registry.

### Code Generation
Always run after changing API types or RBAC:
```bash
make manifests     # Regenerate CRDs, RBAC, webhook configs
make generate      # Regenerate deepcopy methods
make bundle        # Generate OLM bundle (sets version from semver)
```

### Testing Patterns
- **Controller tests**: Use `envtest` with fake clients (see `internal/controller/suite_test.go`)
- **Webhook tests**: Use Ginkgo/Gomega (see `api/v1alpha1/webhook_suite_test.go`)
- **DSProxy tests**: Standard Go tests with httptest (see `cmd/dsproxy/*_test.go`)
- Run `KUBEBUILDER_ASSETS` is auto-downloaded via `setup-envtest` on test run

## Key Patterns & Conventions

### Resource Naming
All resources use `generateNameForComponent(instance, "component")` pattern:
- Format: `{instance.Name}-{component}` (e.g., `grafana-sample-dex`)
- Labels: Always include `app.kubernetes.io/instance`, `app.kubernetes.io/component`, `app.kubernetes.io/name`

### CreateOrUpdate with Retries
**Always** use `CreateOrUpdateWithRetries()` helper (in `utils.go`) instead of raw CreateOrUpdate:
```go
op, err := CreateOrUpdateWithRetries(ctx, r.Client, resource, func() error {
    resource.Spec = desiredSpec
    return ctrl.SetControllerReference(instance, resource, r.Scheme)
})
```
This handles conflict retries with exponential backoff.

### Metrics Convention
Each reconciler (Dex, MariaDB, etc.) defines its own Prometheus metrics:
- Counter: `grafoo_{component}_reconcile_total` with `result` label
- Histogram: `grafoo_{component}_reconcile_duration_seconds`
- Register in `init()` via `metrics.Registry.MustRegister()`

### Webhook Defaulting
Defaults are applied in `api/v1alpha1/grafana_webhook.go` via `GrafooCustomDefaulter`:
- Default datasources defined in `defaults.go` as package vars
- Dex/MariaDB enabled by default unless explicitly disabled
- Validation enforces datasource type matches config (Prometheus → `Prometheus` field, not `Loki`)

### Status Management
Use structured conditions via `meta.SetStatusCondition()`:
- Condition types: `Available`, `DexReady`, `MariaDBReady`, `DataSourcesReady`, `GrafanaReady`
- Update status after each sub-reconciler completes
- Token expiration tracked in `Status.TokenExpirationTime` for requeue logic

## Common Tasks

### Adding a New Datasource Type
1. Add type to `DataSourceType` enum in `api/v1alpha1/grafana_types.go`
2. Add validation in `validateGrafanaDatasources()` webhook
3. Implement `reconcile{Type}DataSource()` in `datasource.go`
4. Add to switch in `ReconcileDataSources()`

### Modifying Default Values
Edit constants in `api/v1alpha1/defaults.go` (imports from `internal/config/config.go` for image references):
- Don't hardcode images in reconcilers—use `DexImage`, `MariaDBImage`, `GrafanaVersion` vars
- Override via flags in `main.go` (`--dex-image`, `--mariadb-image`, `--grafana-version`)

### Debugging Token Issues
Check these in order:
1. `Status.TokenExpirationTime` on Grafana CR
2. Dex config secret annotation `checksum/config.yaml` (triggers pod restart on change)
3. Datasource `SecureJSONData` (extract token via `extractTokenFromSecureJSONData()` helper)
4. Service account token secret format: `{instance.Name}-dex-token` (bound token type)

### Working with DSProxy
- Lives in `cmd/dsproxy`, separate binary from operator
- Uses `iptables` NAT rules to redirect traffic (requires CAP_NET_ADMIN)
- Authorization via OPA policies in `/etc/dsproxy/policy/*.rego`
- JWT validation uses JWKS from OpenShift OAuth (`--jwks-url` flag)
- Config format: `proxies` list with domain and port mappings (see `ProxyRule` struct)

## File Organization
```
api/v1alpha1/          # CRD types, webhook, defaults
internal/controller/   # Reconcilers (controller.go + component-specific files)
internal/config/       # Shared config (image defaults)
cmd/grafoo/            # Operator main
cmd/dsproxy/           # Datasource proxy main + authz
config/                # Kustomize manifests (CRD, RBAC, manager, samples)
bundle/                # OLM bundle (auto-generated)
test/e2e/              # E2E tests
```

## External Dependencies
- **grafana-operator v5**: Grafana/GrafanaDatasource CRDs (not v4!)
- **cert-manager**: Required for webhook certificates
- **OpenShift APIs**: `config.openshift.io` for ingress discovery
- **Kubebuilder**: Project scaffolded with v4, uses controller-runtime 0.18.x

## Release Process
```bash
make release  # Interactive: choose release type, creates branch + bundle + tag
```
Uses `semver` CLI to bump version, creates `release/vX.Y.Z` branch with bundle artifacts.

---

**When in doubt**: Check existing component reconcilers (dex.go, mariadb.go) for patterns. All follow the same structure: metrics → reconcile resources → update status condition.
