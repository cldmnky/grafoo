package controller

import (
	"context"
	"fmt"
	"net/url"

	grafanav1beta1 "github.com/grafana/grafana-operator/v5/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
)

func (r *GrafanaReconciler) ReconcileGrafana(ctx context.Context, instance *grafoov1alpha1.Grafana) error {
	operatedGrafana := &grafanav1beta1.Grafana{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		},
	}
	// TODO: if not dex enabled
	clientSecret, err := r.getClientSecret(ctx, instance)
	if err != nil {
		return err
	}

	// dexRouteDomain is dex route uri
	grafanaRouteUri := r.generateRouteUriForComponent(ctx, instance, "grafana")
	u, err := url.Parse(grafanaRouteUri)
	if err != nil {
		return err
	}
	grafanaRouteDomain := u.Hostname()
	var databaseConfig map[string]string
	if instance.Spec.MariaDB.Enabled {
		// Get the mariadb secret
		mariadbSecret := &corev1.Secret{}
		if err := r.Get(ctx, client.ObjectKey{Name: r.generateNameForComponent(instance, "mariadb"), Namespace: instance.Namespace}, mariadbSecret); err != nil {
			return err
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

	_, err = CreateOrUpdateWithRetries(ctx, r.Client, operatedGrafana, func() error {
		operatedGrafana.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "grafana")
		operatedGrafana.Spec = grafanaSpec
		return ctrl.SetControllerReference(instance, operatedGrafana, r.Scheme)
	})
	if err != nil {
		return err
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
		return err
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
		return err
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
		return err
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
		return err
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
		return err
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
		return err
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
		return err
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
		return err
	}
	return nil
}
