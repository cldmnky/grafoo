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
| `name` _string_ |  |  | Required: {} <br /> |
| `type` _string_ |  |  | Enum: [prometheus-incluster loki-incluster tempo-incluster] <br />Required: {} <br /> |
| `enabled` _boolean_ |  |  | Required: {} <br /> |
| `loki` _[LokiDS](#lokids)_ |  |  | Optional: {} <br /> |
| `tempo` _[TempoDS](#tempods)_ |  |  | Optional: {} <br /> |
| `prometheus` _[PrometheusDS](#prometheusds)_ |  |  | Optional: {} <br /> |


#### Dex







_Appears in:_
- [GrafanaSpec](#grafanaspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `enabled` _boolean_ |  |  |  |
| `image` _string_ |  |  |  |


#### Grafana



Grafana is the Schema for the grafanas API



_Appears in:_
- [GrafanaList](#grafanalist)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `grafoo.cloudmonkey.org/v1alpha1` | | |
| `kind` _string_ | `Grafana` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.22/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[GrafanaSpec](#grafanaspec)_ |  |  |  |


#### GrafanaList



GrafanaList contains a list of Grafana





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `grafoo.cloudmonkey.org/v1alpha1` | | |
| `kind` _string_ | `GrafanaList` | | |
| `metadata` _[ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.22/#listmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `items` _[Grafana](#grafana) array_ |  |  |  |


#### GrafanaSpec



GrafanaSpec defines the desired state of Grafana



_Appears in:_
- [Grafana](#grafana)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `version` _string_ |  |  | Optional: {} <br /> |
| `replicas` _integer_ |  |  | Optional: {} <br /> |
| `tokenDuration` _[Duration](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.22/#duration-v1-meta)_ |  | 1440m0s | Optional: {} <br />Pattern: `^([0-9]+(\.[0-9]+)?(s|m|h))+$` <br />Type: string <br /> |
| `domain` _string_ | IngressDomain is the domain to use for the Grafana Ingress, setting a domain will create an Ingress for Grafana and Dex as grafana.<IngressDomain> and dex.<IngressDomain>. |  | Optional: {} <br />Pattern: `^([a-z0-9]+(-[a-z0-9]+)*\.)+[a-z]{2,}$` <br /> |
| `dex` _[Dex](#dex)_ |  |  | Optional: {} <br /> |
| `mariadb` _[MariaDB](#mariadb)_ |  |  | Optional: {} <br /> |
| `datasources` _[DataSource](#datasource) array_ |  |  |  |




#### LokiDS







_Appears in:_
- [DataSource](#datasource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `url` _string_ |  |  | Optional: {} <br /> |
| `lokiStack` _[LokiStack](#lokistack)_ |  |  | Optional: {} <br /> |


#### LokiStack







_Appears in:_
- [LokiDS](#lokids)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ |  |  | Optional: {} <br /> |
| `namespace` _string_ |  |  | Optional: {} <br /> |


#### MariaDB







_Appears in:_
- [GrafanaSpec](#grafanaspec)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `enabled` _boolean_ |  |  | Optional: {} <br /> |
| `storageSize` _string_ |  |  | Optional: {} <br /> |
| `image` _string_ |  |  | Optional: {} <br /> |


#### PrometheusDS







_Appears in:_
- [DataSource](#datasource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `url` _string_ |  |  | Required: {} <br /> |


#### TempoDS







_Appears in:_
- [DataSource](#datasource)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `url` _string_ |  |  | Optional: {} <br /> |
| `tempoStack` _[TempoStack](#tempostack)_ |  |  | Optional: {} <br /> |


#### TempoStack







_Appears in:_
- [TempoDS](#tempods)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ |  |  | Optional: {} <br /> |
| `namespace` _string_ |  |  | Optional: {} <br /> |


