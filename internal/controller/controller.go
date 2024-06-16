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
	"fmt"
	"net/url"
	"time"

	grafanav1beta1 "github.com/grafana/grafana-operator/v5/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
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

	// Reconcile dex
	if instance.Spec.Dex != nil && instance.Spec.Dex.Enabled {
		if err := r.ReconcileDex(ctx, instance); err != nil {
			logger.Error(err, "Failed to reconcile dex")
			// Set the status to failed
			instance.Status.Phase = grafoov1alpha1.PhaseFailed
			if err := r.Status().Update(ctx, instance); err != nil {
				logger.Error(err, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
	}

	// Reconcile MariaDB
	if instance.Spec.MariaDB != nil && instance.Spec.MariaDB.Enabled {
		if err := r.ReconcileMariaDB(ctx, instance); err != nil {
			logger.Error(err, "Failed to reconcile mariadb")
			// Set the status to failed
			instance.Status.Phase = grafoov1alpha1.PhaseFailed
			if err := r.Status().Update(ctx, instance); err != nil {
				logger.Error(err, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
	}

	// Create a mariadb instance

	// Create a Grafana instance
	grafana := &grafanav1beta1.Grafana{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		},
	}
	// TODO: if not dex enabled
	clientSecret, err := r.getClientSecret(ctx, instance)
	if err != nil {
		return ctrl.Result{}, err
	}

	// dexRouteDomain is dex routeuri withoiut the protocol
	grafanaRouteUri := r.generateRouteUriForComponent(ctx, instance, "grafana")
	u, err := url.Parse(grafanaRouteUri)
	if err != nil {
		return ctrl.Result{}, err
	}
	grafanaRouteDomain := u.Hostname()
	var databaseConfig map[string]string
	if instance.Spec.MariaDB.Enabled {
		// Get the mariadb secret
		mariadbSecret := &corev1.Secret{}
		if err := r.Get(ctx, client.ObjectKey{Name: r.generateNameForComponent(instance, "mariadb"), Namespace: instance.Namespace}, mariadbSecret); err != nil {
			return ctrl.Result{}, err
		}
		mariaDBUrl := fmt.Sprintf("mysql://%s:%s@%s:3306/%s", string(mariadbSecret.Data["database-user"]), string(mariadbSecret.Data["database-password"]), r.generateNameForComponent(instance, "mariadb"), string(mariadbSecret.Data["database-name"]))
		databaseConfig = map[string]string{
			"type": "mysql",
			"url":  mariaDBUrl,
		}
	} else {
		databaseConfig = map[string]string{
			"type": "sqlite3",
		}
	}

	grafanaSpec := grafanav1beta1.GrafanaSpec{
		Version: instance.Spec.Version,
		Deployment: &grafanav1beta1.DeploymentV1{
			Spec: grafanav1beta1.DeploymentV1Spec{
				Replicas: instance.Spec.Replicas,
			},
		},
		Route: &grafanav1beta1.RouteOpenshiftV1{
			Spec: &grafanav1beta1.RouteOpenShiftV1Spec{
				Host: grafanaRouteDomain,
			},
		},
		Config: map[string]map[string]string{
			"server": {
				"root_url": r.generateRouteUriForComponent(ctx, instance, "grafana"),
			},
			"log": {
				"mode":  "console",
				"level": "info",
			},
			"auth": {
				"disable_login_form": "false",
			},
			"auth.generic_oauth": {
				"enabled":                  "true",
				"name":                     "Dex SSO",
				"allow_sign_up":            "true",
				"client_id":                "grafana",
				"client_secret":            clientSecret,
				"scopes":                   "openid email groups",
				"auth_url":                 r.generateRouteUriForComponent(ctx, instance, "dex") + "/auth",
				"token_url":                r.generateRouteUriForComponent(ctx, instance, "dex") + "/token",
				"api_url":                  r.generateRouteUriForComponent(ctx, instance, "dex") + "/userinfo",
				"tls_skip_verify_insecure": "true",
				"role_attribute_path":      "contains(groups[*], 'system:cluster-admins') && 'Admin' || contains(groups[*], 'system:authenticated') && 'Editor' || 'Viewer'",
			},
			"database": databaseConfig,
		},
	}

	_, err = CreateOrUpdateWithRetries(ctx, r.Client, grafana, func() error {
		grafana.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "grafana")
		grafana.Spec = grafanaSpec
		return ctrl.SetControllerReference(instance, grafana, r.Scheme)
	})
	if err != nil {
		return ctrl.Result{}, err
	}
	// Create a clusterrole for the grafana service account that allows subjectaccessreviews
	grafanaClusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: r.generateNameForComponent(instance, "auth-reviewer"),
		},
	}
	grafanaClusterRoleRules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{"authorization.k8s.io"},
			Resources: []string{"subjectaccessreviews"},
			Verbs:     []string{"create"},
		},
		{
			APIGroups: []string{"authorization.k8s.io"},
			Resources: []string{"tokenreviews"},
			Verbs:     []string{"create"},
		},
	}
	_, err = CreateOrUpdateWithRetries(ctx, r.Client, grafanaClusterRole, func() error {
		grafanaClusterRole.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "grafana")
		grafanaClusterRole.Rules = grafanaClusterRoleRules
		return nil
	})
	if err != nil {
		return ctrl.Result{}, err
	}
	// Create a clusterrolebinging for the auth-reviewer clusterrole
	grafanaClusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: r.generateNameForComponent(instance, "auth-reviewer"),
		},
	}
	_, err = CreateOrUpdateWithRetries(ctx, r.Client, grafanaClusterRoleBinding, func() error {
		grafanaClusterRoleBinding.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "grafana")
		grafanaClusterRoleBinding.Subjects = []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      r.generateNameForComponent(instance, "sa"),
				Namespace: instance.Namespace,
			},
		}
		grafanaClusterRoleBinding.RoleRef = rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     r.generateNameForComponent(instance, "auth-reviewer"),
			APIGroup: "rbac.authorization.k8s.io",
		}
		return nil
	})
	if err != nil {
		return ctrl.Result{}, err
	}
	// Create a clusterrole for the grafana service account that allows tempostack reads
	grafanaTempoClusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: r.generateNameForComponent(instance, "tempostack-traces-reader"),
		},
	}
	grafanaTempoClusterRoleRules := []rbacv1.PolicyRule{
		{
			APIGroups:     []string{"tempo.grafana.com"},
			Resources:     []string{"dev"},
			ResourceNames: []string{"traces"},
			Verbs:         []string{"get"},
		},
	}
	_, err = CreateOrUpdateWithRetries(ctx, r.Client, grafanaTempoClusterRole, func() error {
		grafanaTempoClusterRole.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "grafana")
		grafanaTempoClusterRole.Rules = grafanaTempoClusterRoleRules
		return nil
	})
	if err != nil {
		return ctrl.Result{}, err
	}
	// Create a clusterrolebinging for the tempostack-traces-reader clusterrole
	grafanaTempoClusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: r.generateNameForComponent(instance, "tempostack-traces-reader"),
		},
	}
	_, err = CreateOrUpdateWithRetries(ctx, r.Client, grafanaTempoClusterRoleBinding, func() error {
		grafanaTempoClusterRoleBinding.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "grafana")
		grafanaTempoClusterRoleBinding.Subjects = []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      r.generateNameForComponent(instance, "sa"),
				Namespace: instance.Namespace,
			},
		}
		grafanaTempoClusterRoleBinding.RoleRef = rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     r.generateNameForComponent(instance, "tempostack-traces-reader"),
			APIGroup: "rbac.authorization.k8s.io",
		}
		return nil
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	// Create a clusterrolebinding for the grafana service account
	metricsClusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: r.generateNameForComponent(instance, "cluster-monitoring-view"),
		},
	}
	_, err = CreateOrUpdateWithRetries(ctx, r.Client, metricsClusterRoleBinding, func() error {
		metricsClusterRoleBinding.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "grafana")
		metricsClusterRoleBinding.Subjects = []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      r.generateNameForComponent(instance, "sa"),
				Namespace: instance.Namespace,
			},
		}
		metricsClusterRoleBinding.RoleRef = rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     "cluster-monitoring-view",
			APIGroup: "rbac.authorization.k8s.io",
		}
		return nil
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	loggingAppClusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: r.generateNameForComponent(instance, "cluster-logging-application-view"),
		},
	}
	_, err = CreateOrUpdateWithRetries(ctx, r.Client, loggingAppClusterRoleBinding, func() error {
		loggingAppClusterRoleBinding.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "grafana")
		loggingAppClusterRoleBinding.Subjects = []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      r.generateNameForComponent(instance, "sa"),
				Namespace: instance.Namespace,
			},
		}
		loggingAppClusterRoleBinding.RoleRef = rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     "cluster-logging-application-view",
			APIGroup: "rbac.authorization.k8s.io",
		}
		return nil
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	loggingInfraClusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: r.generateNameForComponent(instance, "cluster-logging-infrastructure-view"),
		},
	}
	_, err = CreateOrUpdateWithRetries(ctx, r.Client, loggingInfraClusterRoleBinding, func() error {
		loggingInfraClusterRoleBinding.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "grafana")
		loggingInfraClusterRoleBinding.Subjects = []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      r.generateNameForComponent(instance, "sa"),
				Namespace: instance.Namespace,
			},
		}
		loggingInfraClusterRoleBinding.RoleRef = rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     "cluster-logging-infrastructure-view",
			APIGroup: "rbac.authorization.k8s.io",
		}
		return nil
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	loggingAuditClusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: r.generateNameForComponent(instance, "cluster-logging-audit-view"),
		},
	}
	_, err = CreateOrUpdateWithRetries(ctx, r.Client, loggingAuditClusterRoleBinding, func() error {
		loggingAuditClusterRoleBinding.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "grafana")
		loggingAuditClusterRoleBinding.Subjects = []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      r.generateNameForComponent(instance, "sa"),
				Namespace: instance.Namespace,
			},
		}
		loggingAuditClusterRoleBinding.RoleRef = rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     "cluster-logging-audit-view",
			APIGroup: "rbac.authorization.k8s.io",
		}
		return nil
	})
	if err != nil {
		return ctrl.Result{}, err
	}
	// Create a datasource instance
	if err := r.ReconcileDataSource(ctx, instance); err != nil {
		logger.Error(err, "Failed to reconcile datasource")
		// Set the status to failed
		instance.Status.Phase = grafoov1alpha1.PhaseFailed
		if err := r.Status().Update(ctx, instance); err != nil {
			logger.Error(err, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Update status
	// now + tokenDuration
	tokenExpirationTime := metav1.Time{Time: time.Now().Add(instance.Spec.TokenDuration.Duration)}
	instance.Status.TokenExpirationTime = &tokenExpirationTime
	// Phase
	instance.Status.Phase = grafoov1alpha1.PhaseSucceeded
	if err := r.Status().Update(ctx, instance); err != nil {
		logger.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{Requeue: true, RequeueAfter: instance.Spec.TokenDuration.Duration}, nil
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
		Complete(r)
}
