![alt text](assets/grafoo.png)

*Grafana for OpenShift Observability* - Configure Grafana for use in OpenShift!

`grafoo` deploys Grafana in OpenShift and manages datasources that connects to the in-cluster monitoring and loki logging stack (and in the future to the Multi-Cluster Observability in ACM).

## Requirements

`grafoo` requires that the `grafana-operator` (v5) is installed in the cluster. It also requires `Cert-Manager`to be available in the cluster.

## Deploying

`grafoo` is available in a custom operator catalog (TBA).

It may also be deployed by cloning this repo, creating a `grafoo-system` project and then `make deploy IMG=quay.io/cldmnky/grafoo:latest`.

To deploy a grafoo managed grafana after the prerequisites and the `grafoo`operator has been deployed, simply apply a *CRD* that looks like:

```yaml
apiVersion: grafoo.cloudmonkey.org/v1alpha1
kind: Grafana
metadata:
  labels:
    app.kubernetes.io/name: grafana
    app.kubernetes.io/instance: grafana-sample
    app.kubernetes.io/part-of: grafoo
    app.kubernetes.io/managed-by: kustomize
    app.kubernetes.io/created-by: grafoo
  name: grafana-sample
spec: {}
```

`grafoo` will apply a number of defaults to the CR, these may be changed or applied initially:

```yaml
apiVersion: grafoo.cloudmonkey.org/v1alpha1
kind: Grafana
metadata:
  name: grafana-sample
  namespace: grafoo-system
spec:
  datasources:
  - enabled: true
    name: Prometheus
    prometheus:
      url: https://thanos-querier.openshift-monitoring.svc.cluster.local:9091
    type: prometheus-incluster
  - enabled: true
    loki:
      url: https://logging-loki-gateway-http.openshift-logging.svc.cluster.local:8080/api/logs/v1/application/
    name: Loki (Application)
    type: loki-incluster
  - enabled: true
    loki:
      url: https://logging-loki-gateway-http.openshift-logging.svc.cluster.local:8080/api/logs/v1/infrastructure/
    name: Loki (Infrastructure)
    type: loki-incluster
  - enabled: true
    loki:
      url: https://logging-loki-gateway-http.openshift-logging.svc.cluster.local:8080/api/logs/v1/audit/
    name: Loki (Audit)
    type: loki-incluster
  - enabled: true
    name: Tempo (Dev)
    tempo:
      url: https://tempo-tempo-gateway.openshift-tempo-operator.svc.cluster.local:8080/api/traces/v1/dev/tempo
    type: tempo-incluster
  - enabled: true
    name: Tempo (Prod)
    tempo:
      url: https://tempo-tempo-gateway.openshift-tempo-operator.svc.cluster.local:8080/api/traces/v1/prod/tempo
    type: tempo-incluster
  dex:
    enabled: true
    image: docker.io/dexidp/dex:v2.39.1-distroless
  replicas: 1
  tokenDuration: 24h0m0s
  version: 9.5.17
```

This will result in a Grafana with datasources setup for the in-cluster monitoring and logging, using service account tokens managed by the operator. Authentication is handled by a `Dex IDP` instance. Every user that is `cluster-admin`will have admin access in the grafana instance, all other users will be assigned an `Editor` role.
