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
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var grafanalog = logf.Log.WithName("grafana-resource")

func (r *Grafana) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

//+kubebuilder:webhook:path=/mutate-grafoo-cloudmonkey-org-v1alpha1-grafana,mutating=true,failurePolicy=fail,sideEffects=None,groups=grafoo.cloudmonkey.org,resources=grafanas,verbs=create;update,versions=v1alpha1,name=mgrafana.kb.io,admissionReviewVersions=v1

var _ webhook.Defaulter = &Grafana{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *Grafana) Default() {
	grafanalog.Info("default", "name", r.Name)
	if r.Spec.Version == "" {
		r.Spec.Version = GrafanaVersion
	}
	if r.Spec.Dex == nil {
		r.Spec.Dex = &Dex{
			Enabled: true,
			Image:   DexImage,
		}
	}
	// replicas
	if r.Spec.Replicas == nil {
		r.Spec.Replicas = &GrafanaReplicas
	}
	// datasources
	if len(r.Spec.DataSources) == 0 {
		r.Spec.DataSources = DataSources
	}
	// tokenDuration
	if r.Spec.TokenDuration.Duration == 0 {
		r.Spec.TokenDuration = TokenDuration
	}
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
//+kubebuilder:webhook:path=/validate-grafoo-cloudmonkey-org-v1alpha1-grafana,mutating=false,failurePolicy=fail,sideEffects=None,groups=grafoo.cloudmonkey.org,resources=grafanas,verbs=create;update,versions=v1alpha1,name=vgrafana.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &Grafana{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *Grafana) ValidateCreate() (admission.Warnings, error) {
	grafanalog.Info("validate create", "name", r.Name)

	for _, ds := range r.Spec.DataSources {
		if ds.Type != "prometheus-incluster" && ds.Type != "loki-incluster" && ds.Type != "tempo-incluster" {
			return admission.Warnings{"invalid datasource type"}, nil
		}
	}
	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *Grafana) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	grafanalog.Info("validate update", "name", r.Name)

	// validate datasources type
	for _, ds := range r.Spec.DataSources {
		if ds.Type != "prometheus-incluster" && ds.Type != "loki-incluster" && ds.Type != "tempo-incluster" {
			return admission.Warnings{"invalid datasource type"}, nil
		}
	}
	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *Grafana) ValidateDelete() (admission.Warnings, error) {
	grafanalog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil, nil
}
