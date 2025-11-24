# DSProxy Configuration Plan

## Goal
Dynamically reconfigure the `dsproxy` sidecar based on the datasources defined in the `Grafana` CR.

## Status: ✅ Steps 1-2 Completed

### 1. ✅ Generate DSProxy Configuration
**Location:** `internal/controller/datasource.go` -> `ReconcileDataSources`

**Implementation Details:**
- Added `parseURLHostPort()` function to extract hostname, port, and scheme from datasource URLs
- Added `buildDSProxyConfig()` function to build configuration from all enabled datasources
- Configuration groups ports by domain and scheme (http/https)
- Automatically handles default ports (80 for http, 443 for https)

**Example Generated Configuration:**
```yaml
proxies:
  - domain: prometheus.default.svc.cluster.local
    proxies:
      http: [9090]
  - domain: loki.openshift-logging.svc.cluster.local
    proxies:
      https: [3100]
  - domain: tempo.tempo-system.svc.cluster.local
    proxies:
      https: [3200]
```

### 2. ✅ Manage ConfigMap
**Location:** `internal/controller/datasource.go` -> `reconcileDSProxyConfig()`

**Implementation Details:**
- ConfigMap name: `<instance-name>-dsproxy-config`
- Automatically created/updated during datasource reconciliation
- Contains YAML configuration under key `dsproxy.yaml`
- Has proper labels (`app.kubernetes.io/component: dsproxy`) and owner references
- Automatically cleaned up when Grafana CR is deleted

**Usage:**
The ConfigMap is automatically created by the controller. To mount it in a pod:

```yaml
volumes:
  - name: dsproxy-config
    configMap:
      name: <grafana-instance-name>-dsproxy-config

containers:
  - name: dsproxy
    volumeMounts:
      - name: dsproxy-config
        mountPath: /etc/dsproxy/config
```

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
