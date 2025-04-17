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
	"time"

	grafanav1beta1 "github.com/grafana/grafana-operator/v5/api/v1beta1"
	"github.com/prometheus/client_golang/prometheus"
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
	typeAvailable        = "Available"
	typeDexReady         = "DexReady"
	typeMariaDBReady     = "MariaDBReady"
	typeDataSourcesReady = "DataSourcesReady"
	typeGrafanaReady     = "GrafanaReady"
)

// Metrics
var (
	// GrafanaReconcilerDuration is a histogram metric that tracks the duration of the Grafana reconciler
	GrafanaReconcilerDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "grafana_reconciler_duration_seconds",
			Help:    "Duration of the Grafana reconciler",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10},
		},
		[]string{"namespace", "name"},
	)
	// GrafanaReconcilerErrors is a counter metric that tracks the number of errors in the Grafana reconciler
	GrafanaReconcilerErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grafana_reconciler_errors_total",
			Help: "Total number of errors in the Grafana reconciler",
		},
		[]string{"namespace", "name", "error"},
	)
)

func init() {
	// Register metrics with the global Prometheus registry
	prometheus.MustRegister(GrafanaReconcilerDuration)
	prometheus.MustRegister(GrafanaReconcilerErrors)
}

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

	// Start timing the reconciliation
	start := time.Now()

	// Defer the recording of the reconciliation duration
	defer func() {
		duration := time.Since(start).Seconds()
		GrafanaReconcilerDuration.WithLabelValues(req.Namespace, req.Name).Observe(duration)
	}()

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
		GrafanaReconcilerErrors.WithLabelValues(req.Namespace, req.Name, "failed_to_get_instance").Inc()
		return ctrl.Result{}, err
	}

	// Update initial status
	if len(grafooInstance.Status.Conditions) == 0 {
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
		// Data sources status
		meta.SetStatusCondition(&grafooInstance.Status.Conditions, metav1.Condition{
			Type:    typeDataSourcesReady,
			Status:  metav1.ConditionUnknown,
			Reason:  "DataSourcesNotReconciled",
			Message: "DataSources have not been reconciled",
		})
		// Grafana status
		meta.SetStatusCondition(&grafooInstance.Status.Conditions, metav1.Condition{
			Type:    typeGrafanaReady,
			Status:  metav1.ConditionUnknown,
			Reason:  "GrafanaNotReconciled",
			Message: "Grafana has not been reconciled",
		})

		// Token expiration time, set to -1 hour
		// to force a token generation
		grafooInstance.Status.TokenExpirationTime = &metav1.Time{
			Time: time.Now().Add(-time.Hour),
		}
		if err := r.Status().Update(ctx, grafooInstance); err != nil {
			logger.Error(err, "Failed to update status")
			GrafanaReconcilerErrors.WithLabelValues(req.Namespace, req.Name, "status_update_failed").Inc()
			return ctrl.Result{}, err
		}
		// re-fetch the grafooInstance to get the updated resource version
		if err := r.Get(ctx, req.NamespacedName, grafooInstance); err != nil {
			logger.Error(err, "Failed to get Grafana instance")
			GrafanaReconcilerErrors.WithLabelValues(req.Namespace, req.Name, "failed_to_get_instance").Inc()
			return ctrl.Result{}, err
		}
		logger.Info("Updated initial status")
	}

	// Figure out if we need to refresh tokens
	needsRefresh, err := needsRefreshedToken(grafooInstance)
	if err != nil {
		logger.Error(err, "Failed to check if token needs refresh")
		GrafanaReconcilerErrors.WithLabelValues(req.Namespace, req.Name, "token_refresh_check_failed").Inc()
		return ctrl.Result{}, err
	}

	if needsRefresh {
		logger.Info("Token needs refresh")
		// Update token Creation time
		grafooInstance.Status.TokenGenerationTime = &metav1.Time{
			Time: time.Now(),
		}
		// Update token expiration time
		grafooInstance.Status.TokenExpirationTime = &metav1.Time{
			Time: time.Now().Add(grafooInstance.Spec.TokenDuration.Duration),
		}
	}

	// Reconcile Dex
	if grafooInstance.Spec.Dex != nil {
		if err := r.ReconcileDex(ctx, grafooInstance, needsRefresh); err != nil {
			logger.Error(err, "Failed to reconcile dex")
			GrafanaReconcilerErrors.WithLabelValues(req.Namespace, req.Name, "dex_reconciliation_failed").Inc()
			// status
			meta.SetStatusCondition(&grafooInstance.Status.Conditions, metav1.Condition{
				Type:    typeDexReady,
				Status:  metav1.ConditionFalse,
				Reason:  "DexNotReconciled",
				Message: "Failed to reconcile Dex",
			})
		} else {
			// Update status
			meta.SetStatusCondition(&grafooInstance.Status.Conditions, metav1.Condition{
				Type:    typeDexReady,
				Status:  metav1.ConditionTrue,
				Reason:  "DexReconciled",
				Message: "Dex has been reconciled",
			})
		}
	}

	// Reconcile MariaDB
	if grafooInstance.Spec.MariaDB != nil {
		if err := r.ReconcileMariaDB(ctx, grafooInstance); err != nil {
			logger.Error(err, "Failed to reconcile mariadb")
			GrafanaReconcilerErrors.WithLabelValues(req.Namespace, req.Name, "mariadb_reconciliation_failed").Inc()
			// status
			meta.SetStatusCondition(&grafooInstance.Status.Conditions, metav1.Condition{
				Type:    typeMariaDBReady,
				Status:  metav1.ConditionFalse,
				Reason:  "MariaDBNotReconciled",
				Message: "Failed to reconcile MariaDB",
			})

		} else {
			// Update status
			meta.SetStatusCondition(&grafooInstance.Status.Conditions, metav1.Condition{
				Type:    typeMariaDBReady,
				Status:  metav1.ConditionTrue,
				Reason:  "MariaDBReconciled",
				Message: "MariaDB has been reconciled",
			})
		}
	}
	// Reconcile Grafana
	if err := r.ReconcileGrafana(ctx, grafooInstance); err != nil {
		logger.Error(err, "Failed to reconcile grafana")
		GrafanaReconcilerErrors.WithLabelValues(req.Namespace, req.Name, "grafana_reconciliation_failed").Inc()
		// status
		meta.SetStatusCondition(&grafooInstance.Status.Conditions, metav1.Condition{
			Type:    typeGrafanaReady,
			Status:  metav1.ConditionFalse,
			Reason:  "GrafanaNotReconciled",
			Message: "Failed to reconcile Grafana",
		})
	} else {
		// Update status
		meta.SetStatusCondition(&grafooInstance.Status.Conditions, metav1.Condition{
			Type:    typeGrafanaReady,
			Status:  metav1.ConditionTrue,
			Reason:  "GrafanaReconciled",
			Message: "Grafana has been reconciled",
		})
	}

	// Create a datasource instance
	if err := r.ReconcileDataSources(ctx, grafooInstance, needsRefresh); err != nil {
		logger.Error(err, "Failed to reconcile datasource")
		GrafanaReconcilerErrors.WithLabelValues(req.Namespace, req.Name, "datasources_reconciliation_failed").Inc()
		// status
		meta.SetStatusCondition(&grafooInstance.Status.Conditions, metav1.Condition{
			Type:    typeDataSourcesReady,
			Status:  metav1.ConditionFalse,
			Reason:  "DataSourcesNotReconciled",
			Message: "Failed to reconcile DataSources",
		})
	} else {
		// Update status
		meta.SetStatusCondition(&grafooInstance.Status.Conditions, metav1.Condition{
			Type:    typeDataSourcesReady,
			Status:  metav1.ConditionTrue,
			Reason:  "DataSourcesReconciled",
			Message: "DataSources have been reconciled",
		})
	}
	// Update overall status
	meta.SetStatusCondition(&grafooInstance.Status.Conditions, metav1.Condition{
		Type:    typeAvailable,
		Status:  metav1.ConditionTrue,
		Reason:  "GrafooReconciled",
		Message: "Grafoo has been reconciled",
	})

	// Update the status of the resource
	if err := r.Status().Update(ctx, grafooInstance); err != nil {
		logger.Error(err, "Failed to update status")
		GrafanaReconcilerErrors.WithLabelValues(req.Namespace, req.Name, "status_update_failed").Inc()
		return ctrl.Result{}, err
	}

	return ctrl.Result{Requeue: true, RequeueAfter: grafooInstance.Status.TokenExpirationTime.Sub(time.Time{}) - time.Minute*5}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GrafanaReconciler) SetupWithManager(mgr ctrl.Manager) error {
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
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		WithEventFilter(predicate.ResourceVersionChangedPredicate{}).
		Complete(r)
}
