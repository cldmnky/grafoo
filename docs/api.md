# API Reference

## Packages
- [grafoo.cloudmonkey.org/v1alpha1](#grafoocloudmonkeyorgv1alpha1)


## grafoo.cloudmonkey.org/v1alpha1

Package v1alpha1 contains API Schema definitions for the grafoo v1alpha1 API group

### Resource Types
- [Grafana](#grafana)
- [GrafanaList](#grafanalist)



#### DataSource







_Appears in:_
- [GrafanaSpec](#grafanaspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the DataSource |  | Required: {} <br /> |
| `type` _[DataSourceType](#datasourcetype)_ |  |  | Enum: [prometheus-incluster loki-incluster tempo-incluster prometheus-mcoo] <br />Required: {} <br /> |
| `enabled` _boolean_ |  |  | Required: {} <br /> |
| `loki` _[LokiDS](#lokids)_ | Loki is the configuration for the Loki DataSource |  | Optional: {} <br /> |
| `tempo` _[TempoDS](#tempods)_ | Tempo is the configuration for the Tempo DataSource |  | Optional: {} <br /> |
| `prometheus` _[PrometheusDS](#prometheusds)_ | Prometheus is the configuration for the Prometheus DataSource |  | Optional: {} <br /> |


#### DataSourceType

_Underlying type:_ _string_

DataSourceType defines the type of the data source

_Validation:_
- Enum: [prometheus-incluster loki-incluster tempo-incluster prometheus-mcoo]

_Appears in:_
- [DataSource](#datasource)



#### Dex







_Appears in:_
- [GrafanaSpec](#grafanaspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `enabled` _boolean_ | Enabled is a flag to enable or disable the Dex OIDC provider |  | Required: {} <br /> |
| `image` _string_ | Image is the image to use for the Dex OIDC provider |  | Optional: {} <br /> |


#### Grafana







_Appears in:_
- [GrafanaList](#grafanalist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `grafoo.cloudmonkey.org/v1alpha1` | | |
| `kind` _string_ | `Grafana` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[GrafanaSpec](#grafanaspec)_ |  |  |  |


#### GrafanaList









| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `grafoo.cloudmonkey.org/v1alpha1` | | |
| `kind` _string_ | `GrafanaList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[Grafana](#grafana) array_ |  |  |  |


#### GrafanaSpec



GrafanaSpec defines the desired state of Grafana



_Appears in:_
- [Grafana](#grafana)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `version` _string_ | Version is the version of Grafana to deploy | 9.5.17 | Optional: {} <br />Pattern: `^([0-9]+(\.[0-9]+){0,2})$` <br /> |
| `replicas` _integer_ | Replicas is the number of replicas for the Grafana deployment | 2 | Maximum: 10 <br />Minimum: 1 <br />Required: {} <br /> |
| `tokenDuration` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#duration-v1-meta)_ | TokenDuration is the duration of the token used for authentication<br />The token is used to authenticate for Dex and for the DataSources | 1440m | Optional: {} <br />Pattern: `^([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$` <br />Type: string <br /> |
| `domain` _string_ | IngressDomain is the domain to use for the Grafana Ingress, setting a domain will create an Ingress for Grafana and Dex as grafana.<IngressDomain> and dex.<IngressDomain>. |  | Optional: {} <br />Pattern: `^([a-z0-9]+(-[a-z0-9]+)*\.)+[a-z]{2,}$` <br /> |
| `dex` _[Dex](#dex)_ | Dex is the configuration for the Dex OIDC provider |  | Optional: {} <br /> |
| `mariadb` _[MariaDB](#mariadb)_ | MariaDB is the configuration for the MariaDB database |  | Optional: {} <br /> |
| `enableMCOO` _boolean_ | Enable multicluster observability operator | false | Optional: {} <br /> |
| `datasources` _[DataSource](#datasource) array_ | DataSources is the configuration for the DataSources |  | Optional: {} <br /> |






#### LokiDS







_Appears in:_
- [DataSource](#datasource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `url` _string_ | URL is the URL for the Loki DataSource |  | Optional: {} <br /> |
| `lokiStack` _[LokiStack](#lokistack)_ |  |  | Optional: {} <br /> |


#### LokiStack







_Appears in:_
- [LokiDS](#lokids)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the Loki Stack |  | Optional: {} <br /> |
| `namespace` _string_ | Namespace is the namespace of the Loki Stack |  | Optional: {} <br /> |


#### MariaDB







_Appears in:_
- [GrafanaSpec](#grafanaspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `enabled` _boolean_ | Enabled is a flag to enable or disable the MariaDB database |  | Required: {} <br /> |
| `storageSize` _string_ | StorageSize is the size of the storage for the MariaDB database |  | Optional: {} <br /> |
| `image` _string_ | Image is the image to use for the MariaDB database |  | Optional: {} <br /> |


#### PrometheusDS







_Appears in:_
- [DataSource](#datasource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `url` _string_ | URL is the URL for the Prometheus DataSource |  | Required: {} <br /> |


#### TempoDS







_Appears in:_
- [DataSource](#datasource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `url` _string_ | URL is the URL for the Tempo DataSource |  | Optional: {} <br /> |
| `tempoStack` _[TempoStack](#tempostack)_ | TempoStack is the configuration for the Tempo Stack |  | Optional: {} <br /> |


#### TempoStack







_Appears in:_
- [TempoDS](#tempods)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the name of the Tempo Stack |  | Optional: {} <br /> |
| `namespace` _string_ | Namespace is the namespace of the Tempo Stack |  | Optional: {} <br /> |


