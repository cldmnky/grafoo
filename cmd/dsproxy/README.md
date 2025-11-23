# DSProxy - Prometheus Datasource Proxy with Multi-Tenancy

DSProxy is a transparent HTTP/HTTPS proxy that intercepts traffic to Prometheus datasources and enforces multi-tenancy through **Casbin RBAC authorization** combined with **automatic PromQL label injection**. It uses JWT authentication (via the `sub` claim) to identify users, authorizes access via policy files, and leverages [prom-label-proxy](https://github.com/prometheus-community/prom-label-proxy) to inject authorized namespace labels into all Prometheus queries.

## Overview

DSProxy runs as a sidecar container alongside Grafana or other applications that query Prometheus. It provides:

- **Transparent Traffic Interception**: Uses iptables NAT rules to redirect outbound Prometheus traffic
- **JWT Authentication**: Validates bearer tokens using JWKS from OpenShift OAuth (extracts `sub` claim)
- **Casbin Authorization**: Policy-based access control determining which cluster/namespace pairs users can access
- **Automatic Label Injection**: Injects authorized namespace labels into all PromQL queries via prom-label-proxy
- **Multi-Tenancy Enforcement**: Ensures users only see metrics from namespaces allowed by policy.csv
- **Prometheus API Support**: Handles `/api/v1/query`, `/api/v1/query_range`, `/api/v1/series`, and more
- **Dynamic Configuration**: Hot-reload of iptables rules and authorization policies

## Architecture

```text
┌─────────────┐
│ Application │
│  (Grafana)  │
└──────┬──────┘
       │ PromQL: up{instance="localhost:9090"}
       ↓
┌─────────────────────────┐
│    iptables (NAT)       │
│ Redirects to 127.0.0.1  │
└──────┬──────────────────┘
       │
       ↓
┌─────────────────────────┐
│      DSProxy            │
│  ┌──────────────────┐   │
│  │ Auth Middleware  │   │
│  │  - JWT Verify    │   │
│  │  - Extract sub   │   │
│  │    (user ID)     │   │
│  └────────┬─────────┘   │
│           │             │
│  ┌────────▼─────────┐   │
│  │ Authz Middleware │   │
│  │  - Check policy. │   │
│  │    csv for user  │   │
│  │  - Get allowed   │   │
│  │    namespaces    │   │
│  └────────┬─────────┘   │
│           │             │
│  ┌────────▼─────────┐   │
│  │ prom-label-proxy │   │
│  │  - Parse PromQL  │   │
│  │  - Inject Labels │   │
│  │    {namespace=   │   │
│  │     "tenant-a"}  │   │
│  └────────┬─────────┘   │
│           │             │
│  ┌────────▼─────────┐   │
│  │  Reverse Proxy   │   │
│  │  - Forward Req   │   │
│  └──────────────────┘   │
└──────┬──────────────────┘
       │ PromQL: up{instance="localhost:9090",namespace="tenant-a"}
       ↓
┌─────────────────────────┐
│   Prometheus Server     │
│  (returns only metrics  │
│   matching namespace)   │
└─────────────────────────┘
```

### How It Works

DSProxy enforces multi-tenancy through a pipeline:

1. **JWT Authentication**: Validates the token and extracts the `sub` (subject) claim as the user identity
2. **Casbin Authorization**: Queries `policy.csv` to determine which cluster/namespace pairs the user can access
3. **Label Injection**: Uses prom-label-proxy to automatically inject the authorized namespace into PromQL queries

**Example Transformation:**

Given a policy entry: `p, alice@example.com, datasource1, cluster1/namespace3, read`

Original query from Grafana:

```promql
up{instance="localhost:9090"}
```

After authorization and label injection:

```promql
up{instance="localhost:9090",namespace="namespace3"}
```

This ensures users can **only see metrics** from namespaces authorized in `policy.csv`, providing true multi-tenancy through both authorization and query enforcement.

## Components

### 1. Traffic Interception (`main.go`)

- **iptables Rules**: Creates NAT rules to redirect TCP traffic to local proxy ports
- **Dynamic DNS Resolution**: Resolves domain names to IPs for iptables rules
- **Config Hot-Reload**: Watches config file for changes and updates rules dynamically

**Redirect Ports:**

- HTTP: `5533`
- HTTPS: `5534`

### 2. Authentication (`validate.go`, `handlers.go`)

- **JWKS Initialization**: Fetches public keys from OpenShift OIDC discovery endpoint
- **Token Validation**: Verifies JWT signature, expiration, and audience
- **Identity Extraction**: Extracts `sub` claim as primary user identifier
- **Context Propagation**: Stores user identity and groups in request context

**JWT Claims Used:**

- `sub`: User identifier (used for Casbin policy matching)
- `email`: User email (optional metadata)
- `groups`: User group memberships (used for Casbin role inheritance)
- `aud`: Audience claim (validated)

### 3. Authorization (`authz.go`)

Uses [Casbin](https://casbin.org) for policy-based access control:

- **Policy Model**: RBAC with wildcards and pattern matching
- **Policy File**: `authz/policy.csv` - defines subject → domain → resource → action permissions
- **Resource Format**: `cluster/namespace` (e.g., `cluster1/namespace3`)
- **Wildcards**: Supports `*/*` (all resources), `*/namespace` (namespace in any cluster), `cluster/*` (all namespaces in cluster)
- **Hot-Reload**: Watches policy files for changes and reloads automatically

**Authorization Flow:**

1. Extract `sub` and `groups` from JWT
2. Query Casbin with `(subject, datasource, cluster/namespace, action)`
3. Build list of authorized cluster/namespace pairs
4. Store in request context for label injection

### 4. Label Injection (`handlers.go` + `prom-label-proxy`)

DSProxy integrates [prom-label-proxy](https://github.com/prometheus-community/prom-label-proxy) to automatically inject authorized namespace labels into PromQL queries:

- **Custom Label Extractor**: Reads authorized namespaces from Casbin authorization context
- **PromQL Parsing**: Parses queries from Prometheus API endpoints
- **Automatic Injection**: Adds label matchers like `{namespace="namespace3"}` to all queries based on policy
- **Multi-Namespace Support**: Currently uses first authorized namespace (TODO: support regex for multiple)
- **Supported Endpoints**:
  - `/api/v1/query` - Instant queries
  - `/api/v1/query_range` - Range queries
  - `/api/v1/series` - Series metadata
  - `/api/v1/labels` - Label names
  - `/api/v1/label/<name>/values` - Label values

### 5. Proxy Handler

- **Transparent Proxying**: Forwards modified requests to upstream Prometheus
- **Header Stripping**: Removes `Authorization` header to prevent credential forwarding
- **Label Enforcement**: All queries are automatically filtered by tenant labels

## Configuration

### Environment Variables / Flags

| Flag | Environment Variable | Default | Description |
|------|---------------------|---------|-------------|
| `--config` | `DSPROXY_CONFIG` | `/etc/dsproxy/config/dsproxy.yaml` | Path to proxy configuration |
| `--iptables` | `DSPROXY_IPTABLES` | `true` | Enable iptables traffic interception |
| `--tls-cert` | `DSPROXY_TLS_CERT` | `/etc/dsproxy/tls/tls.crt` | Path to TLS certificate |
| `--tls-key` | `DSPROXY_TLS_KEY` | `/etc/dsproxy/tls/tls.key` | Path to TLS private key |
| `--jwks-url` | `DSPROXY_JWKS_URL` | `https://oidc/.well-known/openid-configuration` | OIDC discovery URL |
| `--policy-path` | `DSPROXY_POLICY_PATH` | `/etc/dsproxy/policy` | Directory containing Casbin policy files |
| `--upstream-url` | `DSPROXY_UPSTREAM_URL` | `http://localhost:9090` | Upstream Prometheus server URL |
| `--injection-label` | `DSPROXY_INJECTION_LABEL` | `namespace` | Label name to inject for multi-tenancy |

### Proxy Configuration (`dsproxy.yaml`)

Defines which domains and ports to intercept:

```yaml
proxies:
  - domain: thanos-querier.openshift-monitoring.svc.cluster.local
    proxies:
      http: [9091]
      https: [9091]
```

### Authorization Policy (`policy.csv`)

Defines which users can access which cluster/namespace pairs. Located in `--policy-path` directory (default: `/etc/dsproxy/policy`).

**Policy Format:**

```csv
# Format: p, subject, domain, object, action
p, system:cluster-admin, *, */*, read
p, alice@example.com, datasource1, cluster1/namespace3, read
p, admin, datasource2, cluster2/namespace-*, read

# Format: g, user, role (role inheritance)
g, alice@example.com, admin
g, bob@example.com, admin
```

**Fields:**

- `subject`: User identifier from JWT `sub` claim or group name
- `domain`: Datasource ID from `X-Datasource-Uid` header (`*` = any datasource)
- `object`: Resource in format `cluster/namespace` (supports wildcards)
- `action`: Operation type (e.g., `read`, `write`)

**Wildcards:**

- `*/*` - All cluster/namespace combinations
- `*/namespace` - Specific namespace in any cluster
- `cluster/*` - All namespaces in specific cluster
- `namespace-*` - Pattern matching (e.g., matches `namespace-dev`, `namespace-prod`)

## Policy Examples and Query Effects

Below are concrete examples showing how different policies affect Prometheus queries:

### Example 1: Single Namespace Access

**Policy:**
```csv
p, alice@example.com, prometheus-prod, cluster1/monitoring, read
```

**JWT sub claim:** `alice@example.com`

**Original Grafana Query:**
```promql
rate(http_requests_total[5m])
```

**Query Sent to Prometheus:**
```promql
rate(http_requests_total{namespace="monitoring"}[5m])
```

**Effect:** Alice can only see HTTP request rates from the `monitoring` namespace.

---

### Example 2: Wildcard Namespace Pattern

**Policy:**
```csv
p, bob@example.com, *, cluster1/dev-*, read
```

**JWT sub claim:** `bob@example.com`

**Original Grafana Query:**
```promql
up{job="api-server"}
```

**Query Sent to Prometheus:**
```promql
up{job="api-server",namespace=~"dev-.*"}
```

**Effect:** Bob can see all services in namespaces matching `dev-*` pattern (e.g., `dev-team-a`, `dev-team-b`).

---

### Example 3: Admin Access (All Namespaces)

**Policy:**
```csv
p, system:cluster-admin, *, */*, read
```

**JWT sub claim:** `system:cluster-admin`

**Original Grafana Query:**
```promql
container_memory_usage_bytes
```

**Query Sent to Prometheus:**
```promql
container_memory_usage_bytes{namespace="*"}
```

**Effect:** Cluster admin sees metrics from **all namespaces** without restriction.

---

### Example 4: Team-Based Access with Role Inheritance

**Policy:**
```csv
# Define team access
p, team-backend, *, cluster1/backend-prod, read
p, team-backend, *, cluster1/backend-staging, read

# Assign users to team
g, alice@example.com, team-backend
g, charlie@example.com, team-backend
```

**JWT sub claim:** `alice@example.com` (inherits `team-backend` role)

**Original Grafana Query:**
```promql
sum(rate(database_queries_total[1m])) by (pod)
```

**Query Sent to Prometheus (first authorized namespace):**
```promql
sum(rate(database_queries_total{namespace="backend-prod"}[1m])) by (pod)
```

**Effect:** Alice (as member of `team-backend`) can query metrics from `backend-prod` and `backend-staging` namespaces. Currently, only the first authorized namespace is injected.

---

### Example 5: Environment-Specific Access

**Policy:**
```csv
# QA team accesses all QA environments
p, qa-team, *, */qa-*, read

# Developers access dev environments
p, developers, *, */dev-*, read

# SRE team has full access
p, sre-team, *, */*, read

# Assign roles
g, david@example.com, qa-team
g, eve@example.com, developers
```

**JWT sub claim (David):** `david@example.com` → `qa-team`

**Original Grafana Query:**
```promql
node_cpu_seconds_total
```

**Query Sent to Prometheus (David/QA):**
```promql
node_cpu_seconds_total{namespace=~"qa-.*"}
```

**JWT sub claim (Eve):** `eve@example.com` → `developers`

**Query Sent to Prometheus (Eve/Developers):**
```promql
node_cpu_seconds_total{namespace=~"dev-.*"}
```

**Effect:** Each team sees only their environment-specific metrics.

---

### Example 6: Multi-Datasource Access

**Policy:**
```csv
# Alice has different access per datasource
p, alice@example.com, prometheus-prod, cluster1/monitoring, read
p, alice@example.com, prometheus-dev, cluster1/*, read
```

**Request with Header:** `X-Datasource-Uid: prometheus-prod`

**JWT sub claim:** `alice@example.com`

**Original Query:**
```promql
up
```

**Query Sent to Prometheus:**
```promql
up{namespace="monitoring"}
```

**Request with Header:** `X-Datasource-Uid: prometheus-dev`

**Query Sent to Prometheus:**
```promql
up{namespace="*"}
```

**Effect:** Alice has restricted access to `prometheus-prod` (monitoring namespace only) but full access to `prometheus-dev` (all namespaces).

---

### Example 7: Unauthorized Access

**Policy:**
```csv
p, alice@example.com, prometheus-prod, cluster1/monitoring, read
# No policy for cluster1/database
```

**JWT sub claim:** `alice@example.com`

**Request:** Query with manual namespace label:
```promql
up{namespace="database"}
```

**Response:** `403 Forbidden`

**Effect:** Authorization middleware checks `policy.csv` and denies access since Alice is not authorized for the `database` namespace. The request never reaches prom-label-proxy.

---

### Important Notes

1. **First Namespace Selection**: When a user is authorized for multiple namespaces, DSProxy currently injects the **first authorized namespace** into queries. A warning is logged:
   ```
   Warning: Multiple namespaces authorized, using first: namespace1
   ```

2. **Wildcard Injection**: For wildcard patterns (e.g., `dev-*`), prom-label-proxy injects a regex matcher: `{namespace=~"dev-.*"}`

3. **Authorization Precedence**: Casbin evaluates policies in order. More specific rules should be defined before general rules.

4. **Header Required**: The `X-Datasource-Uid` header must be present for datasource-specific policies. If missing, wildcard datasource (`*`) policies apply.

5. **Grafana Integration**: When configuring Grafana datasources, set **Custom HTTP Headers** to include `X-Datasource-Uid` with the datasource identifier matching `policy.csv`.

For detailed authorization configuration, policy examples, and troubleshooting, see [authz/README.md](./authz/README.md).

### Policy Model (`authz/model.conf`)

Casbin RBAC model (automatically loaded from `--policy-path`):

```ini
[request_definition]
r = sub, dom, obj, act

[policy_definition]
p = sub, dom, obj, act

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = (g(r.sub, p.sub) || r.sub == p.sub) &&
    (keyMatch2(r.dom, p.dom) || p.dom == "*") &&
    (keyMatch2(r.obj, p.obj) || p.obj == "*") &&
    r.act == p.act
```

### JWT Claims

The proxy uses JWT tokens for authentication and identity:

**Required JWT Claims:**

- `sub`: Subject (user identifier) - **primary identity for authorization**
- `aud`: Audience (validated during authentication)
- `exp`: Expiration timestamp

**Optional JWT Claims:**

- `email`: User email address
- `groups`: Array of group names (used for role-based authorization in Casbin)

**Example JWT Payload:**

```json
{
  "sub": "alice@example.com",
  "email": "alice@example.com",
  "groups": ["team-backend", "developers"],
  "aud": "grafana",
  "exp": 1735000000
}
```

**Authorization Flow:**

1. Extract `sub` from JWT (e.g., `alice@example.com`)
2. Query Casbin: Does `alice@example.com` (or any of her groups) have access to `datasource1` → `cluster1/namespace3` → `read`?
3. Build list of authorized cluster/namespace pairs
4. Extract first authorized namespace and inject into PromQL queries via prom-label-proxy
5. User sees only metrics from authorized namespace(s)

## Usage

### Running Locally

For testing without Kubernetes:

```bash
# Start with custom upstream Prometheus and policy path
go run . --iptables=false \
  --jwks-url=https://oauth-openshift.apps.cluster.local/.well-known/openid-configuration \
  --upstream-url=http://localhost:9090 \
  --injection-label=namespace \
  --policy-path=./cmd/dsproxy/authz

# Create a test policy (policy.csv)
echo "p, testuser@example.com, prometheus-prod, cluster1/monitoring, read" > ./cmd/dsproxy/authz/policy.csv

# Test with JWT (must have 'sub' claim matching policy.csv)
curl -H "Authorization: Bearer <jwt-with-sub-testuser>" \
     -H "X-Datasource-Uid: prometheus-prod" \
     http://localhost:5533/api/v1/query?query=up{job="api"}
```

The proxy will:

1. Validate JWT and extract `sub` claim (e.g., `testuser@example.com`)
2. Check Casbin authorization against `policy.csv` for datasource `prometheus-prod`
3. Find matching policy: `testuser@example.com` → `cluster1/monitoring` → authorized
4. Inject authorized namespace label `monitoring` into query via prom-label-proxy

Query transformation example:

```promql
# Original query from Grafana
up{job="api"}

# After authorization check, query sent to Prometheus
up{job="api",namespace="monitoring"}
```

**Testing Different Scenarios:**

```bash
# Test unauthorized access (no matching policy)
curl -H "Authorization: Bearer <jwt-with-sub-unauthorized-user>" \
     -H "X-Datasource-Uid: prometheus-prod" \
     http://localhost:5533/api/v1/query?query=up
# Expected: 403 Forbidden

# Test wildcard access
echo "p, admin@example.com, *, */*, read" >> ./cmd/dsproxy/authz/policy.csv
curl -H "Authorization: Bearer <jwt-with-sub-admin>" \
     -H "X-Datasource-Uid: prometheus-prod" \
     http://localhost:5533/api/v1/query?query=up
# Expected: 200 OK with namespace="*" injected

# Test role inheritance
echo "g, developer@example.com, team-backend" >> ./cmd/dsproxy/authz/policy.csv
echo "p, team-backend, *, cluster1/backend-prod, read" >> ./cmd/dsproxy/authz/policy.csv
curl -H "Authorization: Bearer <jwt-with-sub-developer>" \
     -H "X-Datasource-Uid: prometheus-prod" \
     http://localhost:5533/api/v1/query?query=up
# Expected: 200 OK with namespace="backend-prod" injected
```

### Running in Kubernetes

Deploy as a sidecar container with Grafana:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: grafana-with-proxy
spec:
  initContainers:
  - name: dsproxy
    image: quay.io/cldmnky/dsproxy:latest
    args:
    - --upstream-url=http://thanos-querier.openshift-monitoring.svc:9091
    - --injection-label=namespace
    - --jwks-url=https://oauth-openshift.apps.cluster.local/.well-known/openid-configuration
    securityContext:
      capabilities:
        add: ["NET_ADMIN"]  # Required for iptables
    volumeMounts:
    - name: config
      mountPath: /etc/dsproxy/config
    - name: tls
      mountPath: /etc/dsproxy/tls
  containers:
  - name: grafana
    image: grafana/grafana:latest
    # Application traffic is automatically intercepted
  volumes:
  - name: config
    configMap:
      name: dsproxy-config
  - name: tls
    secret:
      secretName: dsproxy-tls
```

## Request Flow

1. **Grafana makes Prometheus API request** (e.g., `/api/v1/query?query=up{job="api"}`) with:
   - `Authorization: Bearer <jwt-token>` header
   - `X-Datasource-Uid: prometheus-prod` header (optional, for datasource-specific policies)

2. **iptables intercepts** the request and redirects to `127.0.0.1:5534` (HTTPS) or `127.0.0.1:5533` (HTTP)

3. **Auth middleware** (`authMiddleware` in `handlers.go`) processes the request:
   - Validates JWT signature using JWKS from OIDC provider
   - Checks audience and expiration claims
   - Extracts `sub` claim (e.g., `alice@example.com`) as user identifier
   - Optionally extracts `groups` claim for role inheritance
   - Stores `sub` in request context as `"user"`

4. **Authz middleware** (`authzMiddleware` in `authz.go`) enforces authorization:
   - Extracts datasource ID from `X-Datasource-Uid` header (defaults to `*`)
   - Iterates through all Casbin policies in `policy.csv`
   - Checks if user (via `sub` or `groups`) has access to datasource/cluster/namespace
   - Handles wildcards: `*/*`, `*/namespace`, `cluster/*`, `namespace-*`
   - Builds list of authorized `[[cluster, namespace]]` pairs
   - Stores authorized pairs in request context as `"label_values"`
   - Returns `403 Forbidden` if no matching policy found

5. **prom-label-proxy** (`contextLabelExtractor` in `handlers.go`) transforms the PromQL query:
   - Extracts authorized namespaces from context via `contextLabelExtractor`
   - Selects first authorized namespace (logs warning if multiple)
   - Parses the PromQL query AST
   - Injects `{namespace="authorized-namespace"}` into all metric selectors
   - Example: `up{job="api"}` → `up{job="api",namespace="monitoring"}`
   - For wildcards: `up` → `up{namespace=~"dev-.*"}`

6. **Proxy handler** forwards transformed request to upstream Prometheus:
   - Removes `Authorization` header (token not forwarded to Prometheus)
   - Sends modified query with injected label matcher
   - Streams response back to Grafana

**Multi-Tenancy Enforcement**: 
- **Authorization Layer**: Casbin checks `policy.csv` to determine allowed namespaces
- **Query Layer**: prom-label-proxy injects namespace labels at PromQL AST level
- **Result**: Users can only query metrics from namespaces authorized in `policy.csv`, preventing cross-tenant data access even if they manually specify namespace labels in queries

## Security Considerations

### TLS Verification

Currently, DSProxy **disables TLS verification** when:

- Fetching JWKS from OIDC discovery endpoint
- Connecting to datasources

**Rationale**: OpenShift internal services use self-signed certificates that may not be in the container's trust store.

**Future Enhancement**: Support custom CA bundles for proper certificate validation.

### Token Validation

- Validates JWT signature using RSA public keys from JWKS
- Checks token expiration (`exp` claim)
- Requires specific audience claim (configurable via JWT issuer)
- Does NOT forward the bearer token to upstream Prometheus

**Required JWT Claims:**

- `sub`: User identifier (used for Casbin authorization)
- `aud`: Audience validation
- `exp`: Expiration timestamp

**Optional JWT Claims:**

- `email`: User email (for logging/auditing)
- `groups`: User group memberships (for Casbin role inheritance)

### Authorization Security

**Casbin RBAC Model**: Provides flexible policy-based authorization with:

- Subject-based access control (users and groups)
- Datasource-specific policies
- Wildcard pattern matching with `keyMatch2`
- Role inheritance via `g` directives
- Hot-reload of policies without restart

**Policy Isolation**: Each request is authorized independently based on:

1. JWT `sub` claim (user identity)
2. `X-Datasource-Uid` header (datasource context)
3. Requested cluster/namespace (from policy.csv)

### Label Injection Security

**Protection**: prom-label-proxy enforces label injection at the PromQL AST level, making it **impossible** for users to bypass tenant isolation by crafting queries.

**How It Works:**

- Queries are parsed into Abstract Syntax Tree (AST)
- Authorized namespace label is injected into every metric selector
- Users cannot remove or override the injected label
- Queries contradicting the injected label return no results

**Example:**

```promql
# User sends query (attempting to access another namespace)
up{namespace="unauthorized-namespace"}

# After authorization (user authorized for "monitoring")
up{namespace="monitoring"}  # Original namespace filter is overridden

# Prometheus only returns metrics from "monitoring" namespace
```

**Limitations**:

- Label injection only works for PromQL queries (not for raw API endpoints like `/api/v1/labels`)
- Multi-namespace access currently uses first authorized namespace (with warning logged)
- Admin wildcard access (`*/*`) injects `namespace="*"` which may need Prometheus-side filtering

### Capabilities

Requires `CAP_NET_ADMIN` capability to manipulate iptables rules. This is restricted to the init container in production deployments.

## Testing

```bash
# Run all tests
go test ./cmd/dsproxy/...

# Run with verbose output
go test -v ./cmd/dsproxy/...

# Run specific test suite
go test -v ./cmd/dsproxy/... -run TestPrometheusProxyIntegration

# Run with coverage
go test -coverprofile=coverage.out ./cmd/dsproxy/...
go tool cover -html=coverage.out
```

### Test Suites

The project includes comprehensive tests covering:

1. **TestPrometheusProxyIntegration**: Full pipeline testing (auth → authz → label injection)
2. **TestContextLabelExtractor**: Label extraction from Casbin context
3. **TestNewPrometheusProxy**: Proxy initialization with valid/invalid URLs
4. **TestLabelInjectionWithDifferentQueries**: PromQL query transformation patterns
5. **TestQueryRangeEndpoint**: Range query support
6. **TestMultipleNamespaceScenarios**: Multi-namespace authorization

### Manual Testing

Test authorization and label injection with a local Prometheus instance:

```bash
# Start the proxy (without iptables for testing)
go run . --iptables=false \
  --jwks-url=https://oauth-openshift.apps.cluster.local/.well-known/openid-configuration \
  --upstream-url=http://localhost:9090 \
  --injection-label=namespace \
  --policy-path=./cmd/dsproxy/authz

# Create test policy
cat > ./cmd/dsproxy/authz/policy.csv << EOF
p, testuser@example.com, prometheus-prod, cluster1/test-namespace, read
p, admin@example.com, *, */*, read
p, unauthorized@example.com, prometheus-prod, cluster1/forbidden, read
EOF

# Test 1: Authorized user
curl -H "Authorization: Bearer <jwt-with-sub-testuser>" \
     -H "X-Datasource-Uid: prometheus-prod" \
     http://127.0.0.1:5533/api/v1/query?query=up{job="api"}
# Expected: 200 OK
# Query transformed: up{job="api",namespace="test-namespace"}

# Test 2: Admin with wildcard access
curl -H "Authorization: Bearer <jwt-with-sub-admin>" \
     -H "X-Datasource-Uid: prometheus-prod" \
     http://127.0.0.1:5533/api/v1/query?query=up
# Expected: 200 OK
# Query transformed: up{namespace="*"}

# Test 3: Unauthorized datasource
curl -H "Authorization: Bearer <jwt-with-sub-testuser>" \
     -H "X-Datasource-Uid: prometheus-dev" \
     http://127.0.0.1:5533/api/v1/query?query=up
# Expected: 403 Forbidden (no policy for prometheus-dev)

# Check the Prometheus logs to see the transformed queries
# Or check DSProxy logs for authorization decisions:
# [authz] allowing resource cluster1/test-namespace for subject testuser@example.com
```

**Expected Behavior:**

- Valid JWT with policy match: Query executes with injected namespace label
- Valid JWT but no matching policy: `403 Forbidden` with authorization error
- Missing or invalid JWT: `401 Unauthorized`
- Missing `X-Datasource-Uid` header: Uses wildcard datasource (`*`) from policy

## Troubleshooting

### iptables Rules Not Applied

**Symptom**: Traffic not being intercepted

**Check:**

```bash
# List NAT rules
sudo iptables -t nat -L OUTPUT -n -v

# Should see REDIRECT rules for configured domains
```

**Solution**: Ensure DSProxy is running with root privileges and iptables support is enabled.

---

### JWT Validation Fails

**Symptom**: All requests return `401 Unauthorized`

**Check logs:**

```text
Unauthorized: invalid token signature
```

**Solution:**

- Verify `--jwks-url` points to correct OIDC discovery endpoint
- Check token has correct audience claim
- Ensure token is not expired
- Verify token signature matches JWKS public keys

---

### Authorization Fails (403 Forbidden)

**Symptom**: Valid JWT but requests return `403 Forbidden`

**Check logs:**

```text
[authz] no matching policy found for subject alice@example.com
```

**Debugging Steps:**

1. **Verify JWT sub claim matches policy:**

```bash
# Decode JWT to check sub claim
echo "<jwt-token>" | cut -d'.' -f2 | base64 -d | jq .

# Should show:
# {
#   "sub": "alice@example.com",
#   ...
# }
```

2. **Check policy.csv format:**

```csv
# Correct format (4 fields for policy, 2 for role)
p, alice@example.com, prometheus-prod, cluster1/monitoring, read

# Incorrect - missing fields or wrong format
p, alice@example.com, cluster1/monitoring, read  # WRONG
```

3. **Verify datasource ID:**

```bash
# Check X-Datasource-Uid header matches policy
curl -H "X-Datasource-Uid: prometheus-prod" ...

# Policy must match datasource ID or use wildcard
p, alice@example.com, prometheus-prod, ...  # Specific
p, alice@example.com, *, ...                # Wildcard
```

4. **Test authorization manually:**

```bash
# Enable debug logging (if implemented)
go run . --iptables=false --policy-path=./cmd/dsproxy/authz

# Check logs for authorization decisions:
# [authz] checking policy: p=[alice@example.com prometheus-prod cluster1/monitoring read]
# [authz] subject match: true
# [authz] datasource match: true
# [authz] allowing resource cluster1/monitoring for subject alice@example.com
```

**Common Issues:**

- **Subject mismatch**: JWT `sub` claim doesn't match policy subject
- **Datasource mismatch**: `X-Datasource-Uid` header missing or doesn't match policy domain
- **Wrong resource format**: Must be `cluster/namespace`, not just `namespace`
- **Typo in policy.csv**: Extra spaces, wrong delimiter (must be comma)
- **Case sensitivity**: Subject matching is case-sensitive

---

### Label Injection Not Working

**Symptom**: Queries return data from all namespaces instead of tenant-specific data

**Check logs:**

```text
Warning: Multiple namespaces authorized, using first: namespace1
```

**Debugging:**

```bash
# Verify policy.csv is loaded
ls -la /etc/dsproxy/policy/policy.csv

# Check if namespace is being extracted
# Look for logs: [authz] allowing resource cluster1/test-namespace for subject ...

# Test with verbose logging
curl -v -H "Authorization: Bearer <jwt>" \
     -H "X-Datasource-Uid: prometheus-prod" \
     http://localhost:5533/api/v1/query?query=up
```

**Solution:**

- Ensure Casbin authorization succeeds (check for 200 status, not 403)
- Verify `--injection-label` flag matches the label used in Prometheus metrics (default: `namespace`)
- Check that Prometheus metrics actually have the label (e.g., `up{namespace="monitoring"}`)
- Confirm prom-label-proxy is receiving authorized namespaces from context

---

### Wildcard Patterns Not Matching

**Symptom**: Wildcard policies like `*/dev-*` not working

**Debugging:**

```bash
# Verify model.conf uses keyMatch2 for pattern matching
cat /etc/dsproxy/policy/model.conf

# Matcher should include:
# keyMatch2(r.obj, p.obj) || p.obj == "*"
```

**Common Issues:**

- **Wrong matcher function**: Must use `keyMatch2` for wildcard patterns, not `keyMatch`
- **Pattern syntax**: Use `*` for glob, not regex (e.g., `dev-*`, not `dev-.*`)
- **Order matters**: More specific rules should come before general rules in policy.csv

**Testing wildcards:**

```csv
# These should work with keyMatch2:
p, user, *, */dev-*, read          # Matches any cluster, namespaces like dev-team-a, dev-prod
p, user, *, cluster1/test-*, read  # Matches cluster1, namespaces like test-1, test-2
p, admin, *, */*, read             # Matches everything
```

---

### iptables Rules Conflict

**Symptom**: Other applications using iptables rules experiencing connectivity issues

**Solution:**

- Review OUTPUT chain rules with `sudo iptables -t nat -L OUTPUT -n -v`
- Ensure DSProxy rules are specific to configured domains
- Consider using network namespaces for isolation

---

### Policy Hot-Reload Not Working

**Symptom**: Changes to policy.csv don't take effect

**Check:**

```bash
# Verify file watcher is monitoring policy directory
# Look for logs: [authz] reloading policy from /etc/dsproxy/policy/policy.csv

# Check file permissions
ls -la /etc/dsproxy/policy/policy.csv

# Verify file is being modified (not replaced)
# Some editors create new files instead of modifying, breaking inotify
```

**Solution:**

- Restart DSProxy to force policy reload
- Ensure policy file is writable and in correct location
- Check that policy directory path matches `--policy-path` flag

## Development

### Building from Source

```bash
# Build the binary
go build -o dsproxy ./cmd/dsproxy

# Run tests
go test -v ./cmd/dsproxy/...

# Run with coverage
go test -coverprofile=coverage.out ./cmd/dsproxy/...
go tool cover -html=coverage.out
```

### Customizing Label Extraction

The `contextLabelExtractor` in `handlers.go` implements the `injectproxy.ExtractLabeler` interface from prom-label-proxy. To customize label extraction:

```go
// Example: Extract multiple labels from JWT
type multiLabelExtractor struct{}

func (e *multiLabelExtractor) ExtractLabel(r *http.Request) (string, string, error) {
    namespace := r.Context().Value("namespace").(string)
    team := r.Context().Value("team").(string)
    
    // Return label name and value
    // You can inject multiple labels by chaining proxies
    return "namespace", namespace, nil
}
```

### Integration with Other Identity Providers

Currently supports OpenShift OAuth. To integrate with other OIDC providers:

1. Update `--jwks-url` to point to provider's discovery endpoint
2. Adjust audience claim validation in `authMiddleware()`
3. Ensure JWT includes `namespace` claim (or customize `contextLabelExtractor` to extract from different claim)

## References

- [prom-label-proxy](https://github.com/prometheus-community/prom-label-proxy) - Prometheus label enforcement proxy
- [JWT Best Practices](https://datatracker.ietf.org/doc/html/rfc8725)
- [iptables NAT Tutorial](https://www.netfilter.org/documentation/HOWTO/NAT-HOWTO.html)
- [OIDC Discovery](https://openid.net/specs/openid-connect-discovery-1_0.html)
- [Grafoo Operator](https://github.com/cldmnky/grafoo) - Kubernetes operator for Grafana with integrated observability
