# DSProxy Authorization Configuration

This directory contains the Casbin authorization configuration for DSProxy. Authorization determines which cluster/namespace combinations users can access when querying Prometheus datasources.

## Files

- **`model.conf`**: Casbin RBAC model defining the authorization structure
- **`policy.csv`**: Authorization policies mapping users/groups to allowed resources

## How Authorization Works

DSProxy uses a three-stage authorization process:

1. **JWT Authentication**: Extracts the `sub` (subject) claim from the JWT token as the user identity
2. **Casbin Authorization**: Checks `policy.csv` to find which cluster/namespace pairs the user can access
3. **Label Injection**: Uses prom-label-proxy to inject the authorized namespace into PromQL queries

## Policy Format

Each policy line in `policy.csv` follows this format:

```csv
p, subject, datasource, cluster/namespace, action
```

### Fields

- **subject**: User identifier from JWT `sub` claim or group name
  - Example: `alice@example.com`, `team-backend`, `developers`
  
- **datasource**: Datasource ID from `X-Datasource-Uid` header
  - Use `*` to match any datasource
  - Example: `prometheus-prod`, `datasource1`, `*`
  
- **cluster/namespace**: Resource in format `cluster/namespace`
  - Supports wildcards using `keyMatch2` pattern matching
  - Examples:
    - `cluster1/namespace3` - Exact match
    - `*/dev-*` - Any cluster, namespace starting with "dev-"
    - `prod-cluster/*` - All namespaces in prod-cluster
    - `*/*` - All resources (admin access)
  
- **action**: Operation type
  - Currently: `read`
  - Future: `write`, `delete`, etc.

## Role Inheritance

Users can inherit permissions from groups/roles using the `g` directive:

```csv
g, user, role
```

Example:

```csv
# Define role permissions
p, developers, *, */dev-*, read

# Assign users to role
g, alice@example.com, developers
g, bob@example.com, developers
```

Users `alice@example.com` and `bob@example.com` will inherit all permissions from the `developers` role.

## Wildcard Patterns

The authorization system uses Casbin's `keyMatch2` function for pattern matching:

### Datasource Wildcards

```csv
# Any datasource
p, alice@example.com, *, cluster1/namespace1, read

# Specific datasource
p, bob@example.com, prometheus-prod, cluster1/namespace1, read
```

### Namespace Wildcards

```csv
# Specific namespace in specific cluster
p, user1, *, cluster1/namespace3, read

# All namespaces starting with "dev-" in any cluster
p, developers, *, */dev-*, read

# All namespaces in specific cluster
p, sre-team, *, prod-cluster/*, read

# All resources (full admin access)
p, admin, *, */*, read

# Pattern matching with wildcards
p, team-backend, *, prod-cluster/backend-*, read
# Matches: backend-api, backend-worker, backend-cache, etc.
```

## Common Use Cases

### Team-Based Access

```csv
# Backend team can access all backend namespaces in production
p, team-backend, prometheus-prod, prod-cluster/backend-*, read

# Frontend team can access all frontend namespaces in production
p, team-frontend, prometheus-prod, prod-cluster/frontend-*, read

# Assign users to teams
g, alice@example.com, team-backend
g, bob@example.com, team-frontend
```

### Environment-Based Access

```csv
# Developers can access all dev namespaces in any cluster
p, developers, *, */dev-*, read

# QA team can access all QA namespaces
p, qa-team, *, */qa-*, read

# Production access restricted to SRE team
p, sre-team, *, */prod-*, read
```

### Multi-Namespace Access

Users can have access to multiple namespaces by adding multiple policies:

```csv
# User can access multiple specific namespaces
p, alice@example.com, prometheus-prod, cluster1/monitoring, read
p, alice@example.com, prometheus-prod, cluster1/alerting, read
p, alice@example.com, prometheus-prod, cluster1/logging, read
```

When querying, DSProxy will inject the first authorized namespace. In the future, this will support regex patterns like `namespace=~"monitoring|alerting|logging"`.

### Admin Access

```csv
# Full access to all datasources and namespaces
p, system:cluster-admin, *, */*, read

# Assign admin role
g, admin@example.com, system:cluster-admin
```

## Testing Authorization

You can test authorization policies by:

1. **Check policy file syntax**: Ensure proper CSV format with 4 fields per policy line
2. **Verify role inheritance**: Use `g` directives to test group membership
3. **Test wildcards**: Create patterns and verify they match expected resources
4. **Run integration tests**: Use the test suite in `label_injection_test.go`

### Example Test Scenarios

```bash
# User: alice@example.com
# Groups: team-backend
# Datasource: prometheus-prod
# Query: up{job="api"}

# Policy matches:
p, team-backend, prometheus-prod, prod-cluster/backend-*, read

# Result: Query transformed to:
up{job="api",namespace="backend-api"}
```

## Hot-Reload

DSProxy watches both `model.conf` and `policy.csv` for changes and automatically reloads policies when files are modified. No restart required!

Log output on reload:

```
[authz] detected policy or model change, reloading...
[authz] policy reloaded
```

## Troubleshooting

### User Getting 403 Forbidden

Check:

1. JWT `sub` claim matches a subject in `policy.csv`
2. Datasource ID matches policy (use `*` for any datasource)
3. Cluster/namespace pattern matches policy
4. User has `read` action permission
5. Role inheritance is correct if using groups

Enable debug logging to see authorization decisions:

```
[authz] allowing resource cluster1/namespace3 for subject alice@example.com
[authz] allowed cluster/namespace pairs: [[cluster1 namespace3]]
```

### Policy Not Loading

Check:

1. CSV format is correct (4 fields per line)
2. No syntax errors in `model.conf`
3. File permissions allow reading
4. File watcher has access to directory

## Example Configuration

See the included `policy.csv` for a complete example demonstrating:

- Cluster admin access
- Team-based permissions
- Individual user grants
- Wildcard patterns
- Role inheritance

## References

- [Casbin Documentation](https://casbin.org/docs/overview)
- [Casbin RBAC Model](https://casbin.org/docs/rbac)
- [keyMatch2 Pattern Matching](https://casbin.org/docs/function#keymatch2)
- [DSProxy Documentation](../README.md)
