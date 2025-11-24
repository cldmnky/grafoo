# Grafoo - GitHub Copilot Instructions

## Project Overview
**Grafoo** is an OpenShift Kubernetes operator for deploying Grafana with integrated observability (Prometheus, Loki, Tempo), Dex OIDC, and MariaDB. It leverages `grafana-operator` v5 for the actual Grafana instance management.

## Core Architecture
- **Operator**: Reconciles `Grafana` CRs. Manages sub-components: Dex, MariaDB, Grafana, and Datasources.
- **DSProxy**: A sidecar proxy (`cmd/dsproxy`) enforcing multi-tenancy via JWT auth, Casbin authz, and PromQL label injection.
- **Token Lifecycle**: Uses bound ServiceAccount tokens (not static secrets). The controller refreshes them before expiration (`TokenDuration`).

## Coding Conventions & Patterns

### Reconciliation
- **Create/Update**: Always use `CreateOrUpdateWithRetries(ctx, r.Client, resource, ...)` from `utils.go` to handle conflicts.
- **Naming**: Use `r.generateNameForComponent(instance, "component")` for consistent resource naming (e.g., `{instance}-{component}`).
- **Labels**: Ensure `app.kubernetes.io/instance`, `app.kubernetes.io/component`, and `app.kubernetes.io/name` are set.
- **Status**: Update status conditions using `meta.SetStatusCondition` (types: `DexReady`, `MariaDBReady`, `DataSourcesReady`, `GrafanaReady`).

### DSProxy (Datasource Proxy)
- **Location**: `cmd/dsproxy`.
- **Mechanism**: Intercepts traffic via `iptables` (requires `NET_ADMIN`).
- **AuthZ**: Uses Casbin policies (`policy.csv`) to map users (JWT `sub`) to `cluster/namespace` pairs.
- **Label Injection**: Injects authorized namespace labels into PromQL queries using `prom-label-proxy` logic.

### Configuration
- **Defaults**: Defined in `api/v1alpha1/defaults.go`.
- **Webhooks**: Defaulting and validation logic resides in `api/v1alpha1/grafana_webhook.go`.
- **Images**: Do not hardcode images; use constants from `internal/config` or package variables.

## Development Workflow
- **Build**: `make docker-build` (uses `ko` for multi-arch builds).
- **Run**: `make run` (local controller), `make deploy` (cluster).
- **Generate**: Run `make manifests generate` after modifying API types or RBAC markers.
- **Testing**:
  - **Controller**: Uses `envtest` (see `internal/controller/suite_test.go`).
  - **DSProxy**: Standard Go tests with `httptest`.

## Project Structure
- `cmd/grafoo`: Main operator binary.
- `cmd/dsproxy`: Datasource proxy binary.
- `internal/controller`: Reconcilers (`dex.go`, `mariadb.go`, `grafana.go`, `datasource.go`).
- `api/v1alpha1`: CRD definitions and webhooks.
