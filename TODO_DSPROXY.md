# DSProxy Configuration Plan

## Goal
Dynamically reconfigure the `dsproxy` sidecar based on the datasources defined in the `Grafana` CR.

## Steps

### 1. Generate DSProxy Configuration
**Location:** `internal/controller/datasource.go` -> `ReconcileDataSources`

- After reconciling all datasources, iterate through the active list.
- For each datasource (Prometheus, Loki, Tempo):
  - Parse the URL to extract `Hostname` and `Port`.
  - Determine the protocol (HTTP/HTTPS).
- Construct a `dsproxy.yaml` structure:
  ```yaml
  proxies:
    - domain: <hostname>
      proxies:
        http: [<port>]
        https: [<port>] # if applicable
  ```
- Serialize this structure to YAML.

### 2. Manage ConfigMap
**Location:** `internal/controller/datasource.go` -> `ReconcileDataSources`

- Define a `ConfigMap` name, e.g., `<instance-name>-dsproxy-config`.
- Create or Update this `ConfigMap` in the instance's namespace with the generated YAML under a key (e.g., `dsproxy.yaml`).
- Ensure the `ConfigMap` has appropriate labels and owner references.

### 3. Configure Grafana Deployment
**Location:** `internal/controller/grafana.go` -> `buildGrafanaSpec`

- Modify the `Grafana` CR spec generation to include the `dsproxy` sidecar.
- **Container:**
  - Image: `quay.io/cldmnky/dsproxy:latest` (or configured image).
  - Args:
    - `--config=/etc/dsproxy/config/dsproxy.yaml`
    - `--iptables=true`
    - ... other flags ...
  - SecurityContext: `NET_ADMIN` capability.
- **Volumes:**
  - Add a volume for the `ConfigMap` created in Step 2.
- **VolumeMounts:**
  - Mount the volume to `/etc/dsproxy/config` in the `dsproxy` container.

## Notes
- The `dsproxy` already supports hot-reloading of the configuration file, so updating the ConfigMap should automatically trigger a reload in the sidecar (once the volume projection updates).
