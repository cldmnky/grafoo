# DSProxy - Prometheus Datasource Proxy with Multi-Tenancy

DSProxy is a transparent HTTP/HTTPS proxy that intercepts traffic to Prometheus datasources and enforces multi-tenancy through **Casbin RBAC authorization** combined with **automatic PromQL label injection**. It uses JWT authentication (via the `sub` claim) to identify users, authorizes access via policy files, and leverages [prom-label-proxy](https://github.com/prometheus-community/prom-label-proxy) to inject authorized namespace labels into all Prometheus queries.

## Overview

DSProxy runs as a sidecar container alongside Grafana or other applications that query Prometheus. It provides:

- **Transparent Traffic Interception**: Uses iptables NAT rules to redirect outbound Prometheus traffic
- **JWT Authentication**: Validates bearer tokens using JWKS from OpenShift OAuth (extracts `sub` claim)
- **Casbin Authorization**: Policy-based access control determining which cluster/namespace pairs users can access
- **Automatic Label Injection**: Injects authorized cluster and namespace labels into all PromQL queries via prom-label-proxy
- **Multi-Tenancy Enforcement**: Ensures users only see metrics from namespaces allowed by policy.csv
- **Prometheus API Support**: Handles `/api/v1/query`, `/api/v1/query_range`, `/api/v1/series`, `/api/v1/labels`, and more
- **Dynamic Configuration**: Hot-reload of iptables rules, authorization policies, and the Casbin model
- **Web UI**: React/PatternFly policy editor (served on port 3001, bound to loopback)

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
│ HTTP  → 5533            │
│ HTTPS → 5534            │
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
│  │    {cluster=~    │   │
│  │     "tenant-a",  │   │
│  │     namespace=~  │   │
│  │     "team-a"}    │   │
│  └────────┬─────────┘   │
│           │             │
│  ┌────────▼─────────┐   │
│  │  Reverse Proxy   │   │
│  │  - Forward Req   │   │
│  └──────────────────┘   │
└──────┬──────────────────┘
       │ PromQL: up{instance="localhost:9090",cluster=~"tenant-a",namespace=~"team-a"}
       ↓
┌─────────────────────────┐
│   Prometheus Server     │
│  (returns only metrics  │
│   matching namespace)   │
└─────────────────────────┘
```

### How It Works

DSProxy enforces multi-tenancy through a pipeline:

1. **JWT Authentication**: Validates the token signature, expiration, and audience; extracts the `sub` (subject) claim as the user identity
2. **Casbin Authorization**: Queries `policy.csv` to determine which cluster/namespace pairs the user can access
3. **Label Injection**: Uses prom-label-proxy to inject the authorized cluster and namespace into PromQL queries as **regex matchers**

**Example Transformation:**

Given a policy entry: `p, alice@example.com, datasource1, cluster1/namespace3, read`

Original query from Grafana:

```promql
up{instance="localhost:9090"}
```

After authorization and label injection:

```promql
up{instance="localhost:9090",cluster=~"cluster1",namespace=~"namespace3"}
```

Prometheus regex matchers are fully anchored, so `namespace=~"namespace3"` behaves exactly like `namespace="namespace3"`.

This ensures users can **only see metrics** from the cluster/namespace pairs authorized in `policy.csv`, providing true multi-tenancy through both authorization and query enforcement.

## Components

### 1. Traffic Interception (`main.go`)

- **iptables Rules**: Creates NAT rules in the `nat/OUTPUT` chain that redirect TCP traffic to local proxy ports
- **HTTP vs HTTPS**: HTTP destination ports are redirected to the plain HTTP listener (`5533`); HTTPS destination ports are redirected to the TLS listener (`5534`)
- **Dynamic DNS Resolution**: Resolves domain names to IPs for iptables rules and re-resolves them every 60 seconds so rules track DNS changes
- **Config Hot-Reload**: Watches the config file (including atomic replacements) for changes and applies the difference — rules that are no longer configured are removed
- **Self-Interception Protection**: Rules for the `--upstream-url` hostname are never installed, so DSProxy's own connections to the upstream are not redirected back into itself
- **Cleanup on Shutdown**: All installed rules are removed when DSProxy receives SIGINT/SIGTERM

**Redirect Ports:**

- HTTP: `5533`
- HTTPS: `5534`

> Note: HTTPS interception requires a TLS certificate/key pair. If the configured cert/key files are missing, the HTTPS listener is not started and HTTPS rules would point at a closed port. In that case, either provide TLS material or configure only HTTP ports.

### 2. Authentication (`validate.go`, `handlers.go`)

- **JWKS Initialization**: Fetches public keys from the OpenShift OIDC discovery endpoint
- **Token Validation**: Verifies JWT signature (RSA/ECDSA algorithms only), expiration (`exp` claim **required**), audience (string or array), and optionally issuer
- **Identity Extraction**: Extracts `sub` claim as primary user identifier
- **Context Propagation**: Stores user identity and groups in request context

**JWT Claims Used:**

- `sub`: User identifier (used for Casbin policy matching) - **required**
- `email`: User email (optional metadata)
- `groups`: User group memberships (used for Casbin role inheritance)
- `aud`: Audience claim - **required**, must contain the configured audience
- `exp`: Expiration timestamp - **required**
- `iss`: Issuer - only validated when `--jwt-issuer` is set

### 3. Authorization (`authz.go`)

Uses [Casbin](https://casbin.org) for policy-based access control:

- **Policy Model**: RBAC with wildcards and pattern matching
- **Policy File**: `authz/policy.csv` - defines subject → domain → resource → action permissions
- **Resource Format**: `cluster/namespace` (e.g., `cluster1/namespace3`)
- **Wildcards**: Supports `*/*` (all resources), `*/namespace` (namespace in any cluster), `cluster/*` (all namespaces in cluster), and pattern suffixes like `cluster1/dev-*`
- **Datasource Wildcards**: Datasource domains support `keyMatch2` patterns (e.g., `prometheus-*`)
- **Hot-Reload**: Watches the policy directory; changes to `policy.csv` or `model.conf` are reloaded automatically (no restart required)

**Authorization Flow:**

1. Extract `sub` and `groups` from JWT
2. Iterate policies matching (subject, datasource, action)
3. Build a sorted list of authorized cluster/namespace pairs
4. Store in request context for label injection

### 4. Label Injection (`handlers.go` + `prom-label-proxy`)

DSProxy integrates [prom-label-proxy](https://github.com/prometheus-community/prom-label-proxy) in **regex match mode** to automatically inject the authorized cluster and namespace labels into PromQL queries:

- **Custom Label Extractors**: One extractor per enforced label reads the authorized cluster/namespace pairs from the Casbin authorization context
- **Dual-Label Enforcement**: Two chained prom-label-proxy instances enforce the cluster label (`--cluster-label`, default `cluster`) and the namespace label (`--injection-label`, default `namespace`). Setting `--cluster-label=` disables cluster injection (namespace-only mode).
- **Wildcard Translation**: Casbin glob patterns are translated to PromQL regex:
  - `dev-*` → `dev-.*`
  - `backend-?` → `backend-.`
  - `*` (from `cluster/*` or `*/*`) → `.+`
- **Multi-Resource Support**: All authorized pairs contribute to the injected regexes - cluster regexes and namespace regexes are combined into deterministic (sorted, deduplicated) unions, e.g. `cluster=~"cluster1|cluster2"` and `namespace=~"monitoring|alerting"`
- **Endpoints**:
  - `/api/v1/query` - Instant queries
  - `/api/v1/query_range` - Range queries
  - `/api/v1/series` - Series metadata
  - `/api/v1/labels` - Label names
  - `/api/v1/label/<name>/values` - Label values
  - `/api/v1/query_exemplars` - Exemplars
  - `/federate` - Federated data
  - `/api/v1/alerts` and `/api/v1/rules` - Filtered by tenant labels

### 5. Proxy Handler

- **Transparent Proxying**: Forwards modified requests to the upstream Prometheus
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
| `--jwt-audience` | `DSPROXY_JWT_AUDIENCE` | `example-app` | Expected JWT audience claim |
| `--jwt-issuer` | `DSPROXY_JWT_ISSUER` | *(empty)* | Expected JWT issuer claim (empty = not validated) |
| `--ca-bundle` | `DSPROXY_CA_BUNDLE` | *(empty)* | Path to CA bundle for verifying JWKS and upstream certificates |
| `--policy-path` | `DSPROXY_POLICY_PATH` | `/etc/dsproxy/policy` | Directory containing Casbin policy files |
| `--upstream-url` | `DSPROXY_UPSTREAM_URL` | `http://localhost:9090` | Upstream Prometheus server URL |
| `--injection-label` | `DSPROXY_INJECTION_LABEL` | `namespace` | Label name to inject for namespace multi-tenancy (e.g., `namespace`, `k8s_namespace`) |
| `--cluster-label` | `DSPROXY_CLUSTER_LABEL` | `cluster` | Label name to inject for cluster multi-tenancy (e.g., `cluster`, `k8s_cluster`; empty disables cluster injection) |
| `--ui-port` | `DSPROXY_UI_PORT` | `3001` | Port to serve the web UI (bound to 127.0.0.1) |

### Proxy Configuration (`dsproxy.yaml`)

Defines which domains and ports to intercept:

```yaml
proxies:
  - domain: thanos-querier.openshift-monitoring.svc.cluster.local
    proxies:
      http: [9091]
      https: [9091]
```

- `http` ports are redirected to the HTTP listener (`5533`)
- `https` ports are redirected to the HTTPS listener (`5534`)

### Authorization Policy (`policy.csv`)

Defines which users can access which cluster/namespace pairs. Located in `--policy-path` directory (default: `/etc/dsproxy/policy`), together with `model.conf`.

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
- `action`: Operation type (currently `read`)

**Wildcards:**

- `*/*` - All cluster/namespace combinations
- `*/namespace` - Specific namespace in any cluster
- `cluster/*` - All namespaces in specific cluster
- `namespace-*` - Pattern matching (e.g., matches `namespace-dev`, `namespace-prod`)

## Policy Examples and Query Effects

Below are concrete examples showing how different policies affect Prometheus queries.

### Example 1: Single Namespace Access

**Policy:**
```csv
p, alice@example.com, prometheus-prod, cluster1/monitoring, read
```

**Original Grafana Query:**
```promql
rate(http_requests_total[5m])
```

**Query Sent to Prometheus:**
```promql
rate(http_requests_total{cluster=~"cluster1",namespace=~"monitoring"}[5m])
```

**Effect:** Alice can only see HTTP request rates from the `monitoring` namespace in `cluster1`.

---

### Example 2: Wildcard Namespace Pattern

**Policy:**
```csv
p, bob@example.com, *, cluster1/dev-*, read
```

**Original Grafana Query:**
```promql
up{job="api-server"}
```

**Query Sent to Prometheus:**
```promql
up{job="api-server",cluster=~"cluster1",namespace=~"dev-.*"}
```

**Effect:** Bob can see all services in namespaces matching `dev-*` pattern (e.g., `dev-team-a`, `dev-team-b`) in `cluster1`.

---

### Example 3: Admin Access (All Namespaces)

**Policy:**
```csv
p, system:cluster-admin, *, */*, read
```

**Original Grafana Query:**
```promql
container_memory_usage_bytes
```

**Query Sent to Prometheus:**
```promql
container_memory_usage_bytes{cluster=~".+",namespace=~".+"}
```

**Effect:** Cluster admin sees metrics from all clusters and namespaces (the regexes match any non-empty label value).

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

**Original Grafana Query:**
```promql
sum(rate(database_queries_total[1m])) by (pod)
```

**Query Sent to Prometheus:**
```promql
sum(rate(database_queries_total{cluster=~"cluster1",namespace=~"backend-prod|backend-staging"}[1m])) by (pod)
```

**Effect:** Alice (as member of `team-backend`) can query metrics from **both** `backend-prod` and `backend-staging` namespaces in `cluster1`. All authorized namespaces are injected as a regex union.

---

### Example 5: Environment-Specific Access

**Policy:**
```csv
# QA team accesses all QA environments
p, qa-team, *, */qa-*, read

# Developers access dev environments
p, developers, *, */dev-*, read

# Assign roles
g, david@example.com, qa-team
g, eve@example.com, developers
```

**Query Sent to Prometheus (David/QA):**
```promql
node_cpu_seconds_total{cluster=~".+",namespace=~"qa-.*"}
```

**Query Sent to Prometheus (Eve/Developers):**
```promql
node_cpu_seconds_total{cluster=~".+",namespace=~"dev-.*"}
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

**Query Sent to Prometheus:**
```promql
up{cluster=~"cluster1",namespace=~"monitoring"}
```

**Request with Header:** `X-Datasource-Uid: prometheus-dev`

**Query Sent to Prometheus:**
```promql
up{cluster=~"cluster1",namespace=~".+"}
```

**Effect:** Alice has restricted access to `prometheus-prod` (monitoring namespace in cluster1 only) but full access to `prometheus-dev` (all namespaces in cluster1). Note that DSProxy serves a **single upstream** configured via `--upstream-url`; datasource IDs select authorization policies, not different upstreams.

---

### Example 7: Unauthorized Access

**Policy:**
```csv
p, alice@example.com, prometheus-prod, cluster1/monitoring, read
# No policy for cluster1/database
```

**Request:** Query with manual namespace label:
```promql
up{namespace="database"}
```

**Response:** `200 OK` with the injected matchers ANDed with the user's matcher:
```promql
up{cluster=~"cluster1",namespace="database",namespace=~"monitoring"}
```

**Effect:** No series can satisfy all matchers, so the user gets an empty result. The conflicting user matcher is preserved (regex match mode), which means the query is not rejected, but it also cannot leak data from `database`. If the user has **no** authorized resources at all, the response is `403 Forbidden`.

### Important Notes

1. **Regex Injection**: All authorized cluster and namespace values are injected as single regex matchers (`cluster=~"cluster1|cluster2"`, `namespace=~"ns1|ns2"`). Prometheus regex matchers are fully anchored.
2. **Wildcard Translation**: Casbin glob patterns are translated: `*` → `.*`, `?` → `.`, and a bare `*` (admin) → `.+`.
3. **Deterministic Ordering**: Authorized cluster and namespace regexes are sorted and deduplicated before injection, so the injected matchers are stable across requests.
4. **Label Names Configurable**: The injected label names are configurable via `--injection-label` (namespace) and `--cluster-label` (cluster) - e.g. `k8s_namespace` / `k8s_cluster`.
5. **Header Required**: The `X-Datasource-Uid` header must be present for datasource-specific policies. If missing, wildcard datasource (`*`) policies apply.
6. **Grafana Integration**: When configuring Grafana datasources, set **Custom HTTP Headers** to include `X-Datasource-Uid` with the datasource identifier matching `policy.csv`.

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

The proxy uses JWT tokens for authentication and identity.

**Required JWT Claims:**

- `sub`: Subject (user identifier) - **primary identity for authorization**
- `aud`: Audience - must contain the configured `--jwt-audience` (string or array form)
- `exp`: Expiration timestamp - tokens without `exp` are rejected

**Optional JWT Claims:**

- `email`: User email address
- `groups`: Array of group names (used for role-based authorization in Casbin)
- `iss`: Issuer - validated only when `--jwt-issuer` is configured

**Example JWT Payload:**

```json
{
  "sub": "alice@example.com",
  "email": "alice@example.com",
  "groups": ["team-backend", "developers"],
  "aud": "grafana",
  "iss": "https://oauth-openshift.apps.cluster.local",
  "exp": 1735000000
}
```

**Authorization Flow:**

1. Extract `sub` from JWT (e.g., `alice@example.com`)
2. Query Casbin: Which resources (cluster/namespace) can `alice@example.com` (or any of her groups) access for datasource `datasource1` with action `read`?
3. Build a sorted list of authorized cluster/namespace pairs
4. Translate namespaces into a regex and inject it into PromQL queries via prom-label-proxy
5. User sees only metrics from authorized namespace(s)

## Usage

### Running Locally

For testing without Kubernetes:

```bash
# Start with custom upstream Prometheus and policy path
go run . --iptables=false \
  --jwks-url=https://oauth-openshift.apps.cluster.local/.well-known/openid-configuration \
  --jwt-audience=grafana \
  --upstream-url=http://localhost:9090 \
  --injection-label=namespace \
  --policy-path=./cmd/dsproxy/authz

# Create a test policy (policy.csv)
echo "p, testuser@example.com, prometheus-prod, cluster1/monitoring, read" > ./cmd/dsproxy/authz/policy.csv

# Test with JWT (must have 'sub' claim matching policy.csv, aud=grafana, and exp set)
curl -H "Authorization: Bearer <jwt-with-sub-testuser>" \
     -H "X-Datasource-Uid: prometheus-prod" \
     http://localhost:5533/api/v1/query?query=up{job="api"}
```

The proxy will:

1. Validate JWT and extract `sub` claim (e.g., `testuser@example.com`)
2. Check Casbin authorization against `policy.csv` for datasource `prometheus-prod`
3. Find matching policy: `testuser@example.com` → `cluster1/monitoring` → authorized
4. Inject authorized cluster and namespace labels into the query via prom-label-proxy

Query transformation example:

```promql
# Original query from Grafana
up{job="api"}

# After authorization check, query sent to Prometheus
up{cluster=~"cluster1",job="api",namespace=~"monitoring"}
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
# Expected: 200 OK with cluster=~".+" and namespace=~".+" injected

# Test role inheritance
echo "g, developer@example.com, team-backend" >> ./cmd/dsproxy/authz/policy.csv
echo "p, team-backend, *, cluster1/backend-prod, read" >> ./cmd/dsproxy/authz/policy.csv
curl -H "Authorization: Bearer <jwt-with-sub-developer>" \
     -H "X-Datasource-Uid: prometheus-prod" \
     http://localhost:5533/api/v1/query?query=up
# Expected: 200 OK with cluster=~"cluster1" and namespace=~"backend-prod" injected
```

### Running in Kubernetes

Deploy as a **sidecar container** with Grafana:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: grafana-with-proxy
spec:
  containers:
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
    - name: policy
      mountPath: /etc/dsproxy/policy
    - name: tls
      mountPath: /etc/dsproxy/tls
  - name: grafana
    image: grafana/grafana:latest
    # Application traffic is automatically intercepted
  volumes:
  - name: config
    configMap:
      name: dsproxy-config
  - name: policy
    configMap:
      name: dsproxy-policy    # must contain policy.csv and model.conf
  - name: tls
    secret:
      secretName: dsproxy-tls
```

> DSProxy is a long-running process: it must run as a **regular sidecar**, not an init container. The policy volume must contain both `policy.csv` and `model.conf`.

## Request Flow

1. **Grafana makes Prometheus API request** (e.g., `/api/v1/query?query=up{job="api"}`) with:
   - `Authorization: Bearer <jwt-token>` header
   - `X-Datasource-Uid: prometheus-prod` header (optional, for datasource-specific policies)

2. **iptables intercepts** the request and redirects to `127.0.0.1:5534` (HTTPS) or `127.0.0.1:5533` (HTTP)

3. **Auth middleware** (`authMiddleware` in `handlers.go`) processes the request:
   - Validates JWT signature using JWKS from OIDC provider (RSA/ECDSA only)
   - Requires `exp` and `aud` claims (audience may be a string or array)
   - Optionally validates the `iss` claim (`--jwt-issuer`)
   - Extracts `sub` claim (e.g., `alice@example.com`) as user identifier
   - Extracts `groups` claim for role inheritance
   - Stores `sub` in request context as `"user"`

4. **Authz middleware** (`authzMiddleware` in `authz.go`) enforces authorization:
   - Extracts datasource ID from `X-Datasource-Uid` header (defaults to `*`)
   - Iterates through all Casbin policies in `policy.csv`
   - Checks if user (via `sub` or `groups`) has access to datasource/cluster/namespace
   - Handles wildcards: `*/*`, `*/namespace`, `cluster/*`, `namespace-*`
   - Builds a sorted list of authorized `[[cluster, namespace]]` pairs
   - Stores authorized pairs in request context as `"label_values"`
   - Returns `403 Forbidden` if no matching policy found

5. **prom-label-proxy** (`contextLabelExtractor` in `handlers.go`) transforms the PromQL query:
   - Extracts authorized cluster/namespace pairs from context
   - Translates glob patterns to regex and combines them into sorted unions (e.g., `cluster1|cluster2` and `monitoring|alerting`)
   - Parses the PromQL query AST
   - Injects `{cluster=~"..."}` and `{namespace=~"..."}` into all metric selectors
   - Example: `up{job="api"}` → `up{cluster=~"cluster1",job="api",namespace=~"monitoring"}`
   - For wildcards: `up` → `up{cluster=~".+",namespace=~"dev-.*"}`

6. **Proxy handler** forwards transformed request to upstream Prometheus:
   - Removes `Authorization` header (token not forwarded to Prometheus)
   - Sends modified query with injected label matcher
   - Streams response back to Grafana

**Multi-Tenancy Enforcement**: 
- **Authorization Layer**: Casbin checks `policy.csv` to determine allowed cluster/namespace pairs
- **Query Layer**: prom-label-proxy injects cluster and namespace labels at PromQL AST level
- **Result**: Users can only query metrics from the cluster/namespace pairs authorized in `policy.csv`. If a user manually specifies a conflicting label matcher, the injected matchers are ANDed with it, so the query can never return data from unauthorized resources.

## Security Considerations

### TLS Verification

DSProxy **verifies TLS certificates** by default:

- Fetching JWKS from the OIDC discovery endpoint: uses system trust store
- Connecting to upstream datasources: uses system trust store

For internal services with self-signed certificates, provide a CA bundle:

```bash
dsproxy --ca-bundle=/etc/dsproxy/ca/ca.crt ...
```

The bundle is used for both JWKS and upstream connections.

### Token Validation

- Validates JWT signature using RSA (RS256/384/512) or ECDSA (ES256/384/512) public keys from JWKS. Symmetric algorithms (HS*) are rejected.
- Requires the `exp` claim (tokens without expiration are rejected)
- Requires the `aud` claim to contain the configured audience (string or array form)
- Optionally validates the `iss` claim when `--jwt-issuer` is set
- Does **NOT** forward the bearer token to upstream Prometheus

### Authorization Security

**Casbin RBAC Model**: Provides flexible policy-based authorization with:

- Subject-based access control (users and groups)
- Datasource-specific policies
- Wildcard pattern matching with `keyMatch2`
- Role inheritance via `g` directives
- Hot-reload of policies and model without restart

**Policy Isolation**: Each request is authorized independently based on:

1. JWT `sub` claim (user identity)
2. `X-Datasource-Uid` header (datasource context)
3. Requested cluster/namespace (from policy.csv)

### Label Injection Security

**Protection**: prom-label-proxy enforces label injection at the PromQL AST level, making it **impossible** for users to bypass tenant isolation by crafting queries.

**How It Works:**

- Queries are parsed into Abstract Syntax Tree (AST)
- The authorized cluster and namespace regex matchers are injected into every metric selector
- User-supplied label matchers are preserved and ANDed with the injected matchers, so conflicting matchers can never leak data

**Example:**

```promql
# User sends query (attempting to access another namespace)
up{namespace="unauthorized-namespace"}

# After authorization (user authorized for cluster1/monitoring)
up{cluster=~"cluster1",namespace="unauthorized-namespace",namespace=~"monitoring"}

# No series matches all matchers -> empty result, no data leak
```

### Policy Management UI Security

The web UI is served on `127.0.0.1` only (port 3001). The policy API (`GET/POST /api/policy`) requires the same JWT authentication as the proxy endpoints. Access the UI via port-forward or place it behind an authenticated ingress/OAuth proxy that injects the `Authorization` header.

### Capabilities

Requires `CAP_NET_ADMIN` capability to manipulate iptables rules. The process also needs root (it runs as root in the container image).

## Limitations

- **Single Upstream**: DSProxy proxies to one upstream configured with `--upstream-url`. Datasource IDs (`X-Datasource-Uid`) select authorization policies, not different upstreams.
- **Labels Must Exist on Series**: The injected cluster and namespace labels must be present on the upstream metrics; series without those labels are not returned. Metrics must carry both labels for full multi-tenancy.
- **Label Names**: The enforced label names are fixed per deployment (`--cluster-label` and `--injection-label`). If your metrics use different names (e.g. `k8s_cluster`), configure the flags accordingly - do not mix label names.
- **Alertmanager Silences**: In regex match mode, the Alertmanager silences API returns `501 Not implemented` (prom-label-proxy limitation). This proxy targets Prometheus query APIs.
- **Admin Wildcard**: `*/*` injects `cluster=~".+"` and `namespace=~".+"` which match any non-empty label value.

## Testing

```bash
# Run all tests
go test ./cmd/dsproxy/...

# Run with verbose output
go test -v ./cmd/dsproxy/...

# Run with race detection
go test -race ./cmd/dsproxy/...

# Run with coverage
go test -coverprofile=coverage.out ./cmd/dsproxy/...
go tool cover -html=coverage.out
```

### Test Suites

The project includes tests covering:

1. **TestPrometheusProxyIntegration**: Full pipeline testing (authz → label injection) with exact transformed-query assertions
2. **TestWildcardPolicyInjection**: Casbin glob patterns translated to PromQL regex matchers
3. **TestMultiNamespaceInjection**: Deterministic regex union for multiple authorized namespaces
4. **TestUserNamespaceMatcherIsOverridden**: Conflicting user matchers cannot leak data
5. **TestLabelsAPIEnabled**: `/api/v1/labels` and `/api/v1/label/<name>/values` proxied with injection
6. **TestUpstreamTLSWithCABundle / WithoutCABundle**: Upstream certificate verification
7. **TestPolicyHotReload**: policy.csv changes picked up without restart
8. **TestTokenWithoutExpIsRejected / TestTokenWithArrayAudienceIsAccepted**: JWT claim validation
9. **TestSymmetricSigningAlgorithmsAreRejected**: HS* tokens rejected
10. **TestInitJWKSDiscoveryFailures**: OIDC discovery error handling
11. **TestUIPolicyAPI\***: Policy API authentication, validation, and transactional saves
12. **Config parsing**: Documented `dsproxy.yaml` shape
13. **iptables manager**: HTTPS redirect target, stale-rule removal, cleanup, idempotency

### Manual Testing

Test authorization and label injection with a local Prometheus instance:

```bash
# Start the proxy (without iptables for testing)
go run . --iptables=false \
  --jwks-url=https://oauth-openshift.apps.cluster.local/.well-known/openid-configuration \
  --jwt-audience=grafana \
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
# Query transformed: up{cluster=~"cluster1",job="api",namespace=~"test-namespace"}

# Test 2: Admin with wildcard access
curl -H "Authorization: Bearer <jwt-with-sub-admin>" \
     -H "X-Datasource-Uid: prometheus-prod" \
     http://127.0.0.1:5533/api/v1/query?query=up
# Expected: 200 OK
# Query transformed: up{cluster=~".+",namespace=~".+"}

# Test 3: Unauthorized datasource
curl -H "Authorization: Bearer <jwt-with-sub-testuser>" \
     -H "X-Datasource-Uid: prometheus-dev" \
     http://127.0.0.1:5533/api/v1/query?query=up
# Expected: 403 Forbidden (no policy for prometheus-dev)
```

**Expected Behavior:**

- Valid JWT with policy match: Query executes with injected cluster and namespace regexes
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

**Solution**: Ensure DSProxy is running with root privileges and iptables support is enabled. Verify the config file matches the expected YAML shape (see [Proxy Configuration](#proxy-configuration-dsproxyyaml)).

---

### HTTPS Traffic Fails

**Symptom**: HTTPS requests are redirected but fail

**Check:**

```bash
# Verify the TLS listener is actually running
ss -tlnp | grep 5534

# Check logs for: TLS certificate file not found / TLS key file not found
```

**Solution**: Provide the TLS certificate/key pair (`--tls-cert`, `--tls-key`). HTTPS interception rules redirect to the HTTPS listener (5534), which only starts when both files exist.

---

### JWT Validation Fails

**Symptom**: All requests return `401 Unauthorized`

**Check logs:**

```text
Unauthorized: token is expired
Unauthorized: token has invalid audience
Unauthorized: token has invalid claims: token is missing required claim: exp claim is required
```

**Solution:**

- Verify `--jwks-url` points to correct OIDC discovery endpoint
- Check token has correct audience claim (`--jwt-audience`)
- Ensure token has an `exp` claim and is not expired
- If `--jwt-issuer` is set, the token must carry a matching `iss` claim
- Verify token signature matches JWKS public keys (RSA/ECDSA)

---

### Authorization Fails (403 Forbidden)

**Symptom**: Valid JWT but requests return `403 Forbidden`

**Check logs:**

```text
[authz] no resources allowed for subject alice@example.com
```

**Debugging Steps:**

1. **Verify JWT sub claim matches policy:**

```bash
# Decode JWT to check sub claim
echo "<jwt-token>" | cut -d'.' -f2 | base64 -d | jq .
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

**Common Issues:**

- **Subject mismatch**: JWT `sub` claim doesn't match policy subject
- **Datasource mismatch**: `X-Datasource-Uid` header missing or doesn't match policy domain
- **Wrong resource format**: Must be `cluster/namespace`, not just `namespace`
- **Typo in policy.csv**: Extra spaces, wrong delimiter (must be comma)
- **Case sensitivity**: Subject matching is case-sensitive

---

### Label Injection Not Working

**Symptom**: Queries return data from all namespaces instead of tenant-specific data

**Debugging:**

```bash
# Verify policy.csv is loaded
ls -la /etc/dsproxy/policy/policy.csv

# Look for logs:
# [authz] allowing resource cluster1/test-namespace for subject ...
# [label-injection] Injecting namespace regex: test-namespace
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

- **Pattern syntax**: Use `*` for glob, not regex (e.g., `dev-*`, not `dev-.*`). The glob is translated to a regex (`dev-.*`) for PromQL injection.
- **Order matters**: More specific rules should come before general rules in policy.csv (both are applied; all matching policies contribute authorized namespaces)

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
# Look for logs:
# [authz] watching policy directory: /etc/dsproxy/policy
# [authz] detected change in policy.csv, scheduling reload...
# [authz] policy and model reloaded
```

**Solution:**

- Ensure the policy directory is writable and contains `policy.csv` and `model.conf`
- The watcher watches the **directory**, so both in-place edits and atomic file replacement (rename) are detected
- Check that policy directory path matches `--policy-path` flag

## Development

### Building from Source

```bash
# Build the binary (requires the UI build output, see below)
go build -o bin/dsproxy ./cmd/dsproxy

# Run tests
go test -v ./cmd/dsproxy/...

# Run with coverage
go test -coverprofile=coverage.out ./cmd/dsproxy/...
go tool cover -html=coverage.out
```

### Building the UI and Container Image

The web UI is built with Vite and embedded into the binary via `go:embed`. The production build (`cmd/dsproxy/ui/dist`) is committed to the repository so the Go package compiles on a clean checkout, and can be regenerated with:

```bash
# Build the UI assets
make build-ui

# Build the dsproxy binary with the embedded UI
make build-dsproxy

# Build the container image (UBI9, includes iptables)
make docker-build-dsproxy

# Push the container image
make docker-push-dsproxy
```

### Customizing Label Extraction

The `contextLabelExtractor` in `handlers.go` implements the `injectproxy.ExtractLabeler` interface from prom-label-proxy. To customize label extraction:

```go
// Example: Extract multiple labels from JWT
type multiLabelExtractor struct{}

func (e *multiLabelExtractor) ExtractLabel(next http.HandlerFunc) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        namespace := r.Context().Value("namespace").(string)
        team := r.Context().Value("team").(string)

        // Return label name and value
        // You can inject multiple labels by chaining proxies
        return
    })
}
```

### Integration with Other Identity Providers

Currently supports OpenShift OAuth. To integrate with other OIDC providers:

1. Update `--jwks-url` to point to provider's discovery endpoint
2. Adjust audience claim validation via `--jwt-audience` (and optionally `--jwt-issuer`)
3. Ensure JWT includes `sub` and `exp` claims (or customize `contextLabelExtractor` to extract from a different claim)

## References

- [prom-label-proxy](https://github.com/prometheus-community/prom-label-proxy) - Prometheus label enforcement proxy
- [JWT Best Practices](https://datatracker.ietf.org/doc/html/rfc8725)
- [iptables NAT Tutorial](https://www.netfilter.org/documentation/HOWTO/NAT-HOWTO.html)
- [OIDC Discovery](https://openid.net/specs/openid-connect-discovery-1_0.html)
- [Grafoo Operator](https://github.com/cldmnky/grafoo) - Kubernetes operator for Grafana with integrated observability
