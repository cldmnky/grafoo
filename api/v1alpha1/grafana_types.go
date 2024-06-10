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

	grafanav1beta1 "github.com/grafana/grafana-operator/v5/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GrafanaSpec defines the desired state of Grafana
type GrafanaSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Grafanac grafanav1beta1.GrafanaSpec `json:"grafana,inline"`
	Version  string                     `json:"version,omitempty"`
	Replicas *int32                     `json:"replicas,omitempty"`
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Pattern=`^([a-z0-9]+(-[a-z0-9]+)*\.)+[a-z]{2,}$`
	// IngressDomain is the domain to use for the Grafana Ingress, setting a domain will create an Ingress for Grafana and Dex as grafana.<IngressDomain> and dex.<IngressDomain>.
	IngressDomain string       `json:"domain,omitempty"`
	Dex           *Dex         `json:"dex,omitempty"`
	DataSources   []DataSource `json:"datasources,omitempty"`
}

type Dex struct {
	Enabled bool   `json:"enabled,omitempty"`
	Image   string `json:"image,omitempty"`
}
type DataSource struct {
	// +kubebuilder:validation:Optional
	Name string `json:"name,omitempty"`
	// +kubebuilder:validation:Enum=prometheus-incluster;loki-incluster;tempo-incluster
	Type    string `json:"type,omitempty"`
	URL     string `json:"url,omitempty"`
	Enabled bool   `json:"enabled,omitempty"`
}

func (ds *DataSource) GetDataSourceNameHash() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(ds.Name)))[0:6]
}

// GrafanaStatus defines the observed state of Grafana
type GrafanaStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
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
