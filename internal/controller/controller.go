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

package controller

import (
	"context"

	grafanav1beta1 "github.com/grafana/grafana-operator/v5/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
)

var ()

// GrafanaReconciler reconciles a Grafana object
type GrafanaReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=grafoo.cloudmonkey.org,resources=grafanas,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=grafoo.cloudmonkey.org,resources=grafanas/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=grafoo.cloudmonkey.org,resources=grafanas/finalizers,verbs=update

func (r *GrafanaReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling Grafana")

	// Fetch the Grafana instance
	instance := &grafoov1alpha1.Grafana{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		logger.Error(err, "Failed to get Grafana instance")
		return ctrl.Result{}, err
	}

	// Create a mariadb instance

	// Create a Grafana instance
	grafana := &grafanav1beta1.Grafana{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		},
	}

	op, err := CreateOrUpdateWithRetries(ctx, r.Client, grafana, func() error {
		return ctrl.SetControllerReference(instance, grafana, r.Scheme)
	})
	logger.Info("grafana", "op", op)

	// Create a dex instance for authentication

	// Create a datasource instance

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GrafanaReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&grafoov1alpha1.Grafana{}).
		Owns(&grafanav1beta1.Grafana{}).
		Complete(r)
}
