/*
Copyright 2024 Magnus Bengtsson <magnus@cloudmonkey.org>.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"crypto/sha256"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GrafanaSpec defines the desired state of Grafana
type GrafanaSpec struct {
	// Version is the version of Grafana to deploy
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Pattern="^([0-9]+(\\.[0-9]+){0,2})$"
	// +kubebuilder:default="9.5.17"
	Version string `json:"version,omitempty"`
	// Replicas is the number of replicas for the Grafana deployment
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	// +kubebuilder:default=2
	Replicas *int32 `json:"replicas,omitempty"`
	// TokenDuration is the duration of the token used for authentication
	// The token is used to authenticate for Dex and for the DataSources
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^([0-9]+(\\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$"
	// +kubebuilder:default="1440m"
	TokenDuration *metav1.Duration `json:"tokenDuration,omitempty"`
	// IngressDomain is the domain to use for the Grafana Ingress, setting a domain will create an Ingress for Grafana and Dex as grafana.<IngressDomain> and dex.<IngressDomain>.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Pattern=`^([a-z0-9]+(-[a-z0-9]+)*\.)+[a-z]{2,}$`
	IngressDomain string `json:"domain,omitempty"`
	// Dex is the configuration for the Dex OIDC provider
	// +kubebuilder:validation:Optional
	Dex *Dex `json:"dex,omitempty"`
	// MariaDB is the configuration for the MariaDB database
	// +kubebuilder:validation:Optional
	MariaDB *MariaDB `json:"mariadb,omitempty"`
	// Enable multicluster observability operator
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=false
	EnableMCOO bool `json:"enableMCOO,omitempty"`
	// DataSources is the configuration for the DataSources
	// +kubebuilder:validation:Optional
	DataSources []DataSource `json:"datasources,omitempty"`
}

type MariaDB struct {
	// Enabled is a flag to enable or disable the MariaDB database
	// +kubebuilder:validation:Required
	Enabled bool `json:"enabled,omitempty"`
	// StorageSize is the size of the storage for the MariaDB database
	// +kubebuilder:validation:Optional
	StorageSize string `json:"storageSize,omitempty"`
	// Image is the image to use for the MariaDB database
	// +kubebuilder:validation:Optional
	Image string `json:"image,omitempty"`
}

type Dex struct {
	// Enabled is a flag to enable or disable the Dex OIDC provider
	// +kubebuilder:validation:Required
	Enabled bool `json:"enabled,omitempty"`
	// Image is the image to use for the Dex OIDC provider
	// +kubebuilder:validation:Optional
	Image string `json:"image,omitempty"`
}

// DataSourceType defines the type of the data source
//
// +kubebuilder:validation:Enum=prometheus-incluster;loki-incluster;tempo-incluster;prometheus-mcoo
type DataSourceType string

// ToString converts the DataSourceType to a string
func (d DataSourceType) ToString() string {
	return string(d)
}

const (
	// PrometheusInCluster is the Prometheus data source type
	PrometheusInCluster DataSourceType = "prometheus-incluster"
	// LokiInCluster is the Loki data source type
	LokiInCluster DataSourceType = "loki-incluster"
	// TempoInCluster is the Tempo data source type
	TempoInCluster DataSourceType = "tempo-incluster"
	// PrometheusMcoo is the MCOO data source type
	PrometheusMcoo DataSourceType = "prometheus-mcoo"
)

type DataSource struct {
	// Name is the name of the DataSource
	// +kubebuilder:validation:Required
	Name string `json:"name,omitempty"`
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:tempo-incluster","urn:alm:descriptor:com.tectonic.ui:select:loki-incluster","urn:alm:descriptor:com.tectonic.ui:select:prometheus-incluster","urn:alm:descriptor:com.tectonic.ui:select:prometheus-mcoo"},displayName="DataSource type"
	Type DataSourceType `json:"type,omitempty"`
	// +kubebuilder:validation:Required
	Enabled bool `json:"enabled,omitempty"`
	// Loki is the configuration for the Loki DataSource
	// +kubebuilder:validation:Optional
	Loki *LokiDS `json:"loki,omitempty"`
	// Tempo is the configuration for the Tempo DataSource
	// +kubebuilder:validation:Optional
	Tempo *TempoDS `json:"tempo,omitempty"`
	// Prometheus is the configuration for the Prometheus DataSource
	// +kubebuilder:validation:Optional
	Prometheus *PrometheusDS `json:"prometheus,omitempty"`
}

type LokiDS struct {
	// URL is the URL for the Loki DataSource
	// +kubebuilder:validation:Optional
	URL string `json:"url,omitempty"`
	// +kubebuilder:validation:Optional
	LokiStack *LokiStack `json:"lokiStack,omitempty"`
}

type LokiStack struct {
	// Name is the name of the Loki Stack
	// +kubebuilder:validation:Optional
	Name string `json:"name,omitempty"`
	// Namespace is the namespace of the Loki Stack
	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace,omitempty"`
}

type TempoDS struct {
	// URL is the URL for the Tempo DataSource
	// +kubebuilder:validation:Optional
	URL string `json:"url,omitempty"`
	// TempoStack is the configuration for the Tempo Stack
	// +kubebuilder:validation:Optional
	TempoStack *TempoStack `json:"tempoStack,omitempty"`
}

type TempoStack struct {
	// Name is the name of the Tempo Stack
	// +kubebuilder:validation:Optional
	Name string `json:"name,omitempty"`
	// Namespace is the namespace of the Tempo Stack
	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace,omitempty"`
}

type PrometheusDS struct {
	// URL is the URL for the Prometheus DataSource
	// +kubebuilder:validation:Required
	URL string `json:"url,omitempty"`
}

func (ds *DataSource) GetDataSourceNameHash() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(ds.Name)))[0:6]
}

// GrafanaStatus defines the observed state of Grafana
type GrafanaStatus struct {
	// TokenExpirationTime is the time when the token will expire
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:displayName="Token expiration time"
	TokenExpirationTime *metav1.Time `json:"tokenExpirationTime,omitempty"`
	// TokenGenerationTime is the time when the token was generated
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:displayName="Token generation time"
	TokenGenerationTime *metav1.Time `json:"tokenGenerationTime,omitempty"`
	Phase               string       `json:"phase,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=status
	Conditions []metav1.Condition `json:"conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type Grafana struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GrafanaSpec   `json:"spec,omitempty"`
	Status GrafanaStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type GrafanaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Grafana `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Grafana{}, &GrafanaList{})
}
