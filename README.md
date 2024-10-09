![Grafoo](assets/grafoo-logo-small.png)

# Grafoo

*Grafana for OpenShift Observability* - Configure Grafana for use in OpenShift!

`grafoo` deploys **Grafana** in **OpenShift**, efficiently managing datasources that connect to both the in-cluster monitoring and **Loki** logging stack. This integration ensures a seamless observability experience, providing users with unified monitoring and logging capabilities directly within OpenShift.

### Future Enhancements
In upcoming releases, `grafoo` will extend its capabilities to support **Multi-Cluster Observability** in **Advanced Cluster Management (ACM)**. This enhancement will allow users to monitor and manage multiple clusters from a single Grafana instance, significantly improving scalability and flexibility across distributed environments.

With this feature, teams can achieve consistent observability across various clusters, enabling centralized insights and proactive management of complex, multi-cluster infrastructures.


## Table of Contents
1. [Requirements](#requirements)
2. [Deploying](#deploying)
3. [Example Custom Resource (CR)](#example-custom-resource-cr)
4. [Usage](#usage)
5. [Developing](#developing)
6. [License](#license)

## Requirements

- `grafana-operator` (v5) must be installed in the cluster.
- `Cert-Manager` must be available in the cluster.

## Deploying

`grafoo` is available through a **custom operator catalog** (catalog details will be announced soon). This makes deployment and updates easy through the OperatorHub or OpenShift Console, ensuring that you always have the latest version of `grafoo`.

Alternatively, you can deploy `grafoo` manually by following these steps:

### Manual Deployment

1. **Clone the repository**:
   Begin by cloning the `grafoo` repository to your local machine:
   ```bash
   git clone https://github.com/yourorg/grafoo.git
   cd grafoo
   ```

2. **Create the `grafoo-system` project**:
   In OpenShift, create a namespace (or project) where `grafoo` will be deployed:
   ```bash
   oc new-project grafoo-system
   ```

3. **Install and deploy `grafoo`**:
   Once inside the `grafoo` repository, use the provided Makefile to install and deploy the necessary resources:
   ```bash
   make install
   make deploy
   ```

### Building and Pushing Images Using the Makefile

If you're making changes to `grafoo` or want to build your own images, you can leverage the `Makefile` to simplify the process of building and pushing Docker images. Here are the relevant steps:

1. **Build the Docker image**:
   The following command will build a Docker image for `grafoo`:
   ```bash
   make docker-build
   ```
   This command compiles the necessary code, packages it into a Docker image, and prepares it for deployment.

2. **Push the image to a registry**:
   Once the image is built, you can push it to a container registry (such as Docker Hub, Quay.io, or an internal registry) using the following command:
   ```bash
   make docker-push
   ```
   Before running this command, ensure that the `IMAGE_TAG_BASE` variable is set to your target registry and repository:
   ```bash
   export IMAGE_TAG_BASE=<your-registry>/<your-repo>/grafoo
   ```

3. **Run the image locally (optional)**:
   If you wish to test your image locally without pushing it to a registry, you can use the following command to run it directly on your machine:
   ```bash
   make run
   ```


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
    type: prometheus-incluster # Type is the type of the DataSource
  - enabled: true
    loki:
      url: https://logging-loki-gateway-http.openshift-logging.svc.cluster.local:8080/api/logs/v1/application/
    name: Loki (Application)
    type: loki-incluster # Type is the type of the DataSource
  - enabled: true
    loki:
      url: https://logging-loki-gateway-http.openshift-logging.svc.cluster.local:8080/api/logs/v1/infrastructure/
    name: Loki (Infrastructure)
    type: loki-incluster # Type is the type of the DataSource
  - enabled: true
    loki:
      url: https://logging-loki-gateway-http.openshift-logging.svc.cluster.local:8080/api/logs/v1/audit/
    name: Loki (Audit)
    type: loki-incluster # Type is the type of the DataSource
  - enabled: true
    name: Tempo (Dev)
    tempo:
      url: https://tempo-tempo-gateway.openshift-tempo-operator.svc.cluster.local:8080/api/traces/v1/dev/tempo
    type: tempo-incluster # Type is the type of the DataSource
  - enabled: true
    name: Tempo (Prod)
    tempo:
      url: https://tempo-tempo-gateway.openshift-tempo-operator.svc.cluster.local:8080/api/traces/v1/prod/tempo
    type: tempo-incluster # Type is the type of the DataSource
  dex:
    enabled: true # Enabled is a flag to enable or disable the Dex OIDC provider
    image: docker.io/dexidp/dex:v2.39.1-distroless # Image is the image to use for the Dex OIDC provider
  mariadb:
    enabled: true # Enabled is a flag to enable or disable the MariaDB database
    image: registry.access.redhat.com/rhel9/mariadb-1011:1-12 # Image is the image to use for the MariaDB database
    storageSize: 5Gi # StorageSize is the size of the storage for the MariaDB database
  replicas: 2 # Replicas is the number of replicas for the Grafana deployment
  tokenDuration: 24h0m0s # TokenDuration is the duration of the token used for authentication
  version: 9.5.17 # Version is the version of Grafana to deploy
status:
  phase: Succeeded
  tokenExpirationTime: "2024-06-18T11:21:27Z"
```

This deployment will result in a Grafana instance with pre-configured datasources for in-cluster monitoring and logging. The service account tokens required for these datasources are managed by the operator.

Authentication is facilitated by a `Dex IDP` instance, which integrates with your existing identity provider. Users with `cluster-admin` privileges will automatically receive admin access to the Grafana instance, while all other users will be assigned the `Editor` role.

## Usage

To use grafoo, follow these steps:

1. Ensure that the grafana-operator and Cert-Manager are installed in your cluster.
2. Clone the repository and navigate to the project directory.
3. Create a grafoo-system project in your cluster.
4. Run make install && make deploy to deploy the grafoo operator.
5. Apply a Custom Resource (CR) as shown in the example above to deploy a Grafana instance managed by grafoo.

## License

This project is licensed under the Apache License. See the LICENSE file for details.
