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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GrafanaDataSourceRuleSpec defines the desired state of a rule
type GrafanaDataSourceRuleSpec struct {
	// User is the email/subject of the user (optional if Group is set)
	// +kubebuilder:validation:Optional
	User string `json:"user,omitempty"`
	// Group is the group name (optional if User is set)
	// +kubebuilder:validation:Optional
	Group string `json:"group,omitempty"`

	// DataSourceID matches the Grafana Datasource UID or Name (supports wildcard "*")
	// +kubebuilder:validation:Required
	DataSourceID string `json:"dataSourceId"`

	// Permissions defines the allowed actions and resources
	// +kubebuilder:validation:Required
	Permissions []Permission `json:"permissions"`
}

type Permission struct {
	// Action is the operation allowed (e.g., "read")
	// +kubebuilder:validation:Required
	Action string `json:"action"`
	// Resource is the target cluster/namespace (e.g., "cluster1/default" or "*/kube-system")
	// +kubebuilder:validation:Required
	Resource string `json:"resource"`
}

// GrafanaDataSourceRuleStatus defines the observed state of GrafanaDataSourceRule
type GrafanaDataSourceRuleStatus struct {
	// Conditions represent the latest available observations of the rule's state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// GrafanaDataSourceRule is the Schema for the grafanadatasourcerules API
type GrafanaDataSourceRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GrafanaDataSourceRuleSpec   `json:"spec,omitempty"`
	Status GrafanaDataSourceRuleStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GrafanaDataSourceRuleList contains a list of GrafanaDataSourceRule
type GrafanaDataSourceRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GrafanaDataSourceRule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GrafanaDataSourceRule{}, &GrafanaDataSourceRuleList{})
}
