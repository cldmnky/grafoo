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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
)

// Definitions to manage status conditions
const (
	typeAvailable    = "Available"
	typeDexReady     = "DexReady"
	typeMariaDBReady = "MariaDBReady"
	typeDataSources  = "DataSources"
)

// GrafanaReconciler reconciles a Grafana object
type GrafanaReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Clientset *kubernetes.Clientset
	Dynamic   *dynamic.DynamicClient
}

// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts/token,verbs=get;create
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get
// +kubebuilder:rbac:groups=config.openshift.io,resources=ingresses,verbs=get;list;watch
// +kubebuilder:rbac:groups=grafana.integreatly.org,resources=grafanadatasources,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=grafana.integreatly.org,resources=grafanas,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=grafoo.cloudmonkey.org,resources=grafanas,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=grafoo.cloudmonkey.org,resources=grafanas/finalizers,verbs=update
// +kubebuilder:rbac:groups=grafoo.cloudmonkey.org,resources=grafanas/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=loki.grafana.com,resources=application,resourceNames=logs,verbs=get
// +kubebuilder:rbac:groups=loki.grafana.com,resources=audit,resourceNames=logs,verbs=get
// +kubebuilder:rbac:groups=loki.grafana.com,resources=infrastructure,resourceNames=logs,verbs=get
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=prometheuses/api,resourceNames=k8s,verbs=get;create;update
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create
// +kubebuilder:rbac:groups=authorization.k8s.io,resources=tokenreviews,verbs=create
// +kubebuilder:rbac:groups=tempo.grafana.com,resources=dev,resourceNames=traces,verbs=get
// +kubebuilder:rbac:groups=tempo.grafana.com,resources=prod,resourceNames=traces,verbs=get
// +kubebuilder:rbac:groups=logging.openshift.io,resources=clusterloggings,verbs=get;list;watch
// +kubebuilder:rbac:groups=logging.openshift.io,resources=clusterloggings/status,verbs=get;list;watch

func (r *GrafanaReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling Grafana")

	// Fetch the Grafana grafooInstance
	grafooInstance := &grafoov1alpha1.Grafana{}
	err := r.Get(ctx, req.NamespacedName, grafooInstance)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Grafana resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		logger.Error(err, "Failed to get Grafana instance")
		return ctrl.Result{}, err
	}

	// Update initial status
	if grafooInstance.Status.Conditions == nil || len(grafooInstance.Status.Conditions) == 0 {
		meta.SetStatusCondition(&grafooInstance.Status.Conditions, metav1.Condition{
			Type:    typeAvailable,
			Status:  metav1.ConditionUnknown,
			Reason:  "ReconciliationStarted",
			Message: "Reconciliation has started",
		})
		// Dex status
		meta.SetStatusCondition(&grafooInstance.Status.Conditions, metav1.Condition{
			Type:    typeDexReady,
			Status:  metav1.ConditionUnknown,
			Reason:  "DexNotReconciled",
			Message: "Dex has not been reconciled",
		})
		// MariaDB status
		meta.SetStatusCondition(&grafooInstance.Status.Conditions, metav1.Condition{
			Type:    typeMariaDBReady,
			Status:  metav1.ConditionUnknown,
			Reason:  "MariaDBNotReconciled",
			Message: "MariaDB has not been reconciled",
		})
		if err := r.Status().Update(ctx, grafooInstance); err != nil {
			logger.Error(err, "Failed to update status")
			return ctrl.Result{}, err
		}
		// re-fetch the grafooInstance to get the updated resource version
		if err := r.Get(ctx, req.NamespacedName, grafooInstance); err != nil {
			logger.Error(err, "Failed to get Grafana instance")
			return ctrl.Result{}, err
		}
		logger.Info("Updated initial status")
	}

	// Reconcile Dex
	if grafooInstance.Spec.Dex != nil && grafooInstance.Spec.Dex.Enabled {
		if err := r.ReconcileDex(ctx, grafooInstance); err != nil {
			logger.Error(err, "Failed to reconcile dex")
			// status
			meta.SetStatusCondition(&grafooInstance.Status.Conditions, metav1.Condition{
				Type:    typeDexReady,
				Status:  metav1.ConditionFalse,
				Reason:  "DexNotReconciled",
				Message: "Failed to reconcile Dex",
			})
			// Update status
			if err := r.Status().Update(ctx, grafooInstance); err != nil {
				logger.Error(err, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		// Update status
		meta.SetStatusCondition(&grafooInstance.Status.Conditions, metav1.Condition{
			Type:    typeDexReady,
			Status:  metav1.ConditionTrue,
			Reason:  "DexReconciled",
			Message: "Dex has been reconciled",
		})
		if err := r.Status().Update(ctx, grafooInstance); err != nil {
			logger.Error(err, "Failed to update status")
			return ctrl.Result{}, err
		}
	}

	// Reconcile MariaDB
	if err := r.ReconcileMariaDB(ctx, grafooInstance); err != nil {
		logger.Error(err, "Failed to reconcile mariadb")
		// status
		meta.SetStatusCondition(&grafooInstance.Status.Conditions, metav1.Condition{
			Type:    typeMariaDBReady,
			Status:  metav1.ConditionFalse,
			Reason:  "MariaDBNotReconciled",
			Message: "Failed to reconcile MariaDB",
		})
		// Update status
		if err := r.Status().Update(ctx, grafooInstance); err != nil {
			logger.Error(err, "Failed to update status")
		}
		return ctrl.Result{}, err
	}
	// Update status
	meta.SetStatusCondition(&grafooInstance.Status.Conditions, metav1.Condition{
		Type:    typeMariaDBReady,
		Status:  metav1.ConditionTrue,
		Reason:  "MariaDBReconciled",
		Message: "MariaDB has been reconciled",
	})
	if err := r.Status().Update(ctx, grafooInstance); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	// Reconcile Grafana
	if err := r.ReconcileGrafana(ctx, grafooInstance); err != nil {
		logger.Error(err, "Failed to reconcile grafana")
		// status
		meta.SetStatusCondition(&grafooInstance.Status.Conditions, metav1.Condition{
			Type:    typeAvailable,
			Status:  metav1.ConditionFalse,
			Reason:  "GrafanaNotReconciled",
			Message: "Failed to reconcile Grafana",
		})
		// Update status
		if err := r.Status().Update(ctx, grafooInstance); err != nil {
			logger.Error(err, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Create a datasource instance
	if err := r.ReconcileDataSources(ctx, grafooInstance); err != nil {
		logger.Error(err, "Failed to reconcile datasource")
		// status
		meta.SetStatusCondition(&grafooInstance.Status.Conditions, metav1.Condition{
			Type:    typeDataSources,
			Status:  metav1.ConditionFalse,
			Reason:  "DataSourcesNotReconciled",
			Message: "Failed to reconcile DataSources",
		})
		return ctrl.Result{}, err
	}
	// Update status
	meta.SetStatusCondition(&grafooInstance.Status.Conditions, metav1.Condition{
		Type:    typeDataSources,
		Status:  metav1.ConditionTrue,
		Reason:  "DataSourcesReconciled",
		Message: "DataSources have been reconciled",
	})
	// Update overall status
	meta.SetStatusCondition(&grafooInstance.Status.Conditions, metav1.Condition{
		Type:    typeAvailable,
		Status:  metav1.ConditionTrue,
		Reason:  "GrafanaReconciled",
		Message: "Grafana has been reconciled",
	})
	if err := r.Status().Update(ctx, grafooInstance); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{Requeue: true, RequeueAfter: grafooInstance.Spec.TokenDuration.Duration}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GrafanaReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ignoreStatusUpdate := predicate.GenerationChangedPredicate{}
	return ctrl.NewControllerManagedBy(mgr).
		For(&grafoov1alpha1.Grafana{}).
		Owns(&grafanav1beta1.Grafana{}).
		Owns(&grafanav1beta1.GrafanaDatasource{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.Service{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&networkingv1.Ingress{}).
		WithEventFilter(ignoreStatusUpdate).
		Complete(r)
}
