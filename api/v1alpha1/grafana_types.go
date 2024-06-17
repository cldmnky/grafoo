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
	// +kubebuilder:validation:Optional
	Version string `json:"version,omitempty"`
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	// +kubebuilder:default=2
	Replicas *int32 `json:"replicas,omitempty"`
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^([0-9]+(\\.[0-9]+)?(s|m|h))+$"
	// +kubebuilder:default="1440m0s"
	TokenDuration metav1.Duration `json:"tokenDuration,omitempty"`
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Pattern=`^([a-z0-9]+(-[a-z0-9]+)*\.)+[a-z]{2,}$`
	// IngressDomain is the domain to use for the Grafana Ingress, setting a domain will create an Ingress for Grafana and Dex as grafana.<IngressDomain> and dex.<IngressDomain>.
	IngressDomain string `json:"domain,omitempty"`
	// +kubebuilder:validation:Optional
	Dex *Dex `json:"dex,omitempty"`
	// +kubebuilder:validation:Optional
	MariaDB     *MariaDB     `json:"mariadb,omitempty"`
	DataSources []DataSource `json:"datasources,omitempty"`
}

type MariaDB struct {
	// +kubebuilder:validation:Optional
	Enabled bool `json:"enabled,omitempty"`
	// +kubebuilder:validation:Optional
	StorageSize string `json:"storageSize,omitempty"`
	// +kubebuilder:validation:Optional
	Image string `json:"image,omitempty"`
}

type Dex struct {
	Enabled bool   `json:"enabled,omitempty"`
	Image   string `json:"image,omitempty"`
}
type DataSource struct {
	// +kubebuilder:validation:Required
	Name string `json:"name,omitempty"`
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=prometheus-incluster;loki-incluster;tempo-incluster
	Type string `json:"type,omitempty"`
	// +kubebuilder:validation:Required
	Enabled bool `json:"enabled,omitempty"`
	// +kubebuilder:validation:Optional
	Loki *LokiDS `json:"loki,omitempty"`
	// +kubebuilder:validation:Optional
	Tempo *TempoDS `json:"tempo,omitempty"`
	// +kubebuilder:validation:Optional
	Prometheus *PrometheusDS `json:"prometheus,omitempty"`
}

type LokiDS struct {
	// +kubebuilder:validation:Optional
	URL string `json:"url,omitempty"`
	// +kubebuilder:validation:Optional
	LokiStack *LokiStack `json:"lokiStack,omitempty"`
}

type LokiStack struct {
	// +kubebuilder:validation:Optional
	Name string `json:"name,omitempty"`
	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace,omitempty"`
}

type TempoDS struct {
	// +kubebuilder:validation:Optional
	URL string `json:"url,omitempty"`
	// +kubebuilder:validation:Optional
	TempoStack *TempoStack `json:"tempoStack,omitempty"`
}

type TempoStack struct {
	// +kubebuilder:validation:Optional
	Name string `json:"name,omitempty"`
	// +kubebuilder:validation:Optional
	Namespace string `json:"namespace,omitempty"`
}

type PrometheusDS struct {
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
	TokenExpirationTime *metav1.Time `json:"tokenExpirationTime,omitempty"`
	Phase               string       `json:"phase,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Grafana is the Schema for the grafanas API
type Grafana struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GrafanaSpec   `json:"spec,omitempty"`
	Status GrafanaStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GrafanaList contains a list of Grafana
type GrafanaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Grafana `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Grafana{}, &GrafanaList{})
}
