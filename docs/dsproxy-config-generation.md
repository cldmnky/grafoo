# DSProxy Configuration Generation

The Grafana operator automatically generates dsproxy configuration based on the datasources defined in your Grafana custom resource. This configuration is stored in a ConfigMap that can be mounted into the dsproxy sidecar container.

## How It Works

When you create or update a Grafana CR with datasources, the controller:

1. Parses each datasource URL to extract the hostname, port, and scheme (http/https)
2. Groups the ports by domain
3. Generates a YAML configuration in the dsproxy format
4. Creates/updates a ConfigMap named `<grafana-instance-name>-dsproxy-config`

## Example

Given this Grafana CR:

```yaml
apiVersion: grafoo.cloudmonkey.org/v1alpha1
kind: Grafana
metadata:
  name: my-grafana
spec:
  datasources:
    - name: Prometheus
      type: prometheus-incluster
      enabled: true
      prometheus:
        url: http://prometheus.default.svc.cluster.local:9090
    - name: Loki
      type: loki-incluster
      enabled: true
      loki:
        url: https://loki.openshift-logging.svc.cluster.local:3100
    - name: Tempo
      type: tempo-incluster
      enabled: true
      tempo:
        url: https://tempo.tempo-system.svc.cluster.local:3200
```

The controller will automatically create a ConfigMap named `my-grafana-dsproxy-config` with this content:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-grafana-dsproxy-config
  namespace: default
  labels:
    app.kubernetes.io/name: grafana
    app.kubernetes.io/instance: my-grafana
    app.kubernetes.io/component: dsproxy
data:
  dsproxy.yaml: |
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

## Using the ConfigMap with DSProxy

To use the generated configuration with the dsproxy sidecar container, mount the ConfigMap:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: grafana-with-dsproxy
spec:
  volumes:
    - name: dsproxy-config
      configMap:
        name: my-grafana-dsproxy-config
  
  containers:
    - name: dsproxy
      image: quay.io/cldmnky/dsproxy:latest
      args:
        - --config=/etc/dsproxy/config/dsproxy.yaml
        - --iptables=true
        - --jwks-url=https://oauth-openshift.apps.cluster.local/.well-known/openid-configuration
        - --policy-path=/etc/dsproxy/policy
        - --token-review=true
        - --jwt-audience=grafana
      securityContext:
        capabilities:
          add: ["NET_ADMIN"]  # Required for iptables manipulation
      volumeMounts:
        - name: dsproxy-config
          mountPath: /etc/dsproxy/config
    
    - name: grafana
      image: grafana/grafana:latest
      # Grafana's outbound traffic to datasources will be automatically intercepted
      # by the dsproxy iptables rules
```

## Configuration Features

- **Automatic Updates**: The ConfigMap is automatically updated when you add, remove, or modify datasources in the Grafana CR
- **Port Grouping**: Multiple datasources pointing to the same domain but different ports will be grouped together
- **Scheme Separation**: HTTP and HTTPS ports are separated in the configuration
- **Default Ports**: If no port is specified in the URL, the controller uses default ports (80 for HTTP, 443 for HTTPS)
- **Owner References**: The ConfigMap has an owner reference to the Grafana CR, so it will be automatically deleted when the Grafana CR is deleted

## Disabling Datasources

If you set `enabled: false` on a datasource, it will be excluded from the dsproxy configuration:

```yaml
datasources:
  - name: Prometheus
    type: prometheus-incluster
    enabled: false  # This datasource will not be included in dsproxy config
    prometheus:
      url: http://prometheus.default.svc.cluster.local:9090
```

## Troubleshooting

### ConfigMap not created

Check the controller logs:

```bash
kubectl logs -n grafoo-system deployment/grafoo-controller-manager -c manager
```

Look for messages like:
- `Created dsproxy ConfigMap`
- `Updated dsproxy ConfigMap`
- `Failed to reconcile dsproxy config`

### ConfigMap is empty or has wrong content

1. Verify your datasources have valid URLs with scheme (http:// or https://)
2. Check that datasources are enabled (`enabled: true`)
3. Verify the datasource type has the correct configuration field (Prometheus → `prometheus`, Loki → `loki`, Tempo → `tempo`)

### Traffic not being intercepted

1. Ensure the dsproxy init container ran successfully
2. Check iptables rules in the pod:
   ```bash
   kubectl exec -it <pod-name> -c dsproxy -- iptables -t nat -L OUTPUT
   ```
3. Verify the ConfigMap is mounted at `/etc/dsproxy/config/dsproxy.yaml`
4. Check dsproxy logs for configuration loading messages

## Example: Multiple Ports per Domain

If you have multiple datasources pointing to the same domain with different ports:

```yaml
datasources:
  - name: Prometheus-1
    type: prometheus-incluster
    enabled: true
    prometheus:
      url: http://prometheus.default.svc.cluster.local:9090
  - name: Prometheus-2
    type: prometheus-incluster
    enabled: true
    prometheus:
      url: https://prometheus.default.svc.cluster.local:9091
```

The generated configuration will group them:

```yaml
proxies:
  - domain: prometheus.default.svc.cluster.local
    proxies:
      http: [9090]
      https: [9091]
```

## Hot Reload

The dsproxy supports hot-reloading of the configuration file. When you update the datasources in your Grafana CR:

1. The controller updates the ConfigMap
2. Kubernetes propagates the ConfigMap update to mounted volumes (can take up to 60 seconds)
3. The dsproxy file watcher detects the change
4. New iptables rules are automatically applied

No pod restart is required for configuration updates!
