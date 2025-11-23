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
	"github.com/cldmnky/grafoo/internal/config"
)

func (r *GrafanaReconciler) ReconcileGrafana(ctx context.Context, instance *grafoov1alpha1.Grafana) error {
	// Get client secret
	clientSecret, err := r.getClientSecret(ctx, instance)
	if err != nil {
		return err
	}

	// Get database configuration
	databaseConfig, err := r.getDatabaseConfig(ctx, instance)
	if err != nil {
		return err
	}

	// Define subjects for ClusterRoleBindings
	subjects := []rbacv1.Subject{
		{
			Kind:      "ServiceAccount",
			Name:      r.generateNameForComponent(instance, "sa"),
			Namespace: instance.Namespace,
		},
	}

	// Create ClusterRoles and ClusterRoleBindings
	if err := r.createAuthReviewerResources(ctx, instance, subjects); err != nil {
		return err
	}

	if err := r.createTempoStackResources(ctx, instance, subjects); err != nil {
		return err
	}

	if err := r.createMonitoringViewBinding(ctx, instance, subjects); err != nil {
		return err
	}

	if err := r.createLoggingBindings(ctx, instance, subjects); err != nil {
		return err
	}

	// Create SCC binding for dsproxy
	if instance.Spec.EnableDSProxy {
		if err := r.createSCCBinding(ctx, instance, subjects); err != nil {
			return err
		}
		if err := r.createDSProxyPermissions(ctx, instance, subjects); err != nil {
			return err
		}
	}

	// Build GrafanaSpec
	grafanaSpec, err := r.buildGrafanaSpec(ctx, instance, clientSecret, databaseConfig)
	if err != nil {
		return err
	}

	// Create or update Grafana resource
	if err := r.createOrUpdateGrafanaResource(ctx, instance, grafanaSpec); err != nil {
		return err
	}

	return nil
}

// getDatabaseConfig retrieves the database configuration for a Grafana instance.
// If MariaDB is enabled in the Grafana spec, it fetches the MariaDB credentials from a Kubernetes Secret
// and constructs a MySQL connection URL. Otherwise, it defaults to using SQLite3.
//
// Parameters:
// - ctx: The context for the request.
// - instance: The Grafana instance for which the database configuration is being retrieved.
//
// Returns:
// - A map containing the database type and connection URL (if applicable).
// - An error if there is an issue retrieving the MariaDB credentials.
func (r *GrafanaReconciler) getDatabaseConfig(ctx context.Context, instance *grafoov1alpha1.Grafana) (map[string]string, error) {
	if instance.Spec.MariaDB.Enabled {
		mariadbSecret := &corev1.Secret{}
		if err := r.Get(ctx, client.ObjectKey{Name: r.generateNameForComponent(instance, "mariadb"), Namespace: instance.Namespace}, mariadbSecret); err != nil {
			return nil, err
		}
		mariaDBURL := fmt.Sprintf("mysql://%s:%s@%s:3306/%s",
			string(mariadbSecret.Data["database-user"]),
			string(mariadbSecret.Data["database-password"]),
			r.generateNameForComponent(instance, "mariadb"),
			string(mariadbSecret.Data["database-name"]),
		)
		return map[string]string{
			"type": "mysql",
			"url":  mariaDBURL,
		}, nil
	}
	return map[string]string{
		"type": "sqlite3",
	}, nil
}

// buildGrafanaSpec constructs the GrafanaSpec for a Grafana instance.
// It generates the route URI for the Grafana component, parses it to extract the domain,
// and configures various settings including server, log, authentication, and database configurations.
//
// Parameters:
//
//	ctx - The context for the request.
//	instance - The Grafana instance for which the spec is being built.
//	clientSecret - The client secret for OAuth authentication.
//	databaseConfig - A map containing database configuration settings.
//
// Returns:
//
//	grafanav1beta1.GrafanaSpec - The constructed Grafana specification.
//	error - An error if any occurred during the construction of the spec.
func (r *GrafanaReconciler) buildGrafanaSpec(ctx context.Context, instance *grafoov1alpha1.Grafana, clientSecret string, databaseConfig map[string]string) (grafanav1beta1.GrafanaSpec, error) {
	grafanaRouteURI := r.generateRouteUriForComponent(ctx, instance, "grafana")
	u, err := url.Parse(grafanaRouteURI)
	if err != nil {
		return grafanav1beta1.GrafanaSpec{}, err
	}
	grafanaRouteDomain := u.Hostname()

	deploymentSpec := grafanav1beta1.DeploymentV1Spec{
		Replicas: instance.Spec.Replicas,
	}

	if instance.Spec.EnableDSProxy {
		deploymentSpec.Template = &grafanav1beta1.DeploymentV1PodTemplateSpec{
			Spec: &grafanav1beta1.DeploymentV1PodSpec{
				ServiceAccountName: r.generateNameForComponent(instance, "sa"),
				Containers: []corev1.Container{
					{
						Name:  "dsproxy",
						Image: config.DSProxyImage,
						SecurityContext: &corev1.SecurityContext{
							Capabilities: &corev1.Capabilities{
								Add: []corev1.Capability{"NET_ADMIN"},
							},
						},
						Args: []string{
							"--config=/etc/dsproxy/config/dsproxy.yaml",
							"--iptables=true",
							"--jwks-url=" + r.generateRouteUriForComponent(ctx, instance, "dex") + "/.well-known/openid-configuration",
							"--policy-path=/etc/dsproxy/policy",
							"--token-review=true",
							"--jwt-audience=grafana",
						},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "dsproxy-config",
								MountPath: "/etc/dsproxy/config",
							},
							{
								Name:      "dsproxy-tls",
								MountPath: "/etc/dsproxy/tls",
							},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "dsproxy-config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: r.generateNameForComponent(instance, "dsproxy-config"),
								},
							},
						},
					},
					{
						Name: "dsproxy-tls",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: r.generateNameForComponent(instance, "dsproxy-tls"),
							},
						},
					},
				},
			},
		}
	}

	return grafanav1beta1.GrafanaSpec{
		Version: instance.Spec.Version,
		Deployment: &grafanav1beta1.DeploymentV1{
			Spec: deploymentSpec,
		},
		Service: &grafanav1beta1.ServiceV1{
			ObjectMeta: grafanav1beta1.ObjectMeta{
				Annotations: map[string]string{
					"service.beta.openshift.io/serving-cert-secret-name": r.generateNameForComponent(instance, "dsproxy-tls"),
				},
			},
		},
		Route: &grafanav1beta1.RouteOpenshiftV1{
			Spec: &grafanav1beta1.RouteOpenShiftV1Spec{
				Host: grafanaRouteDomain,
			},
		},
		Config: map[string]map[string]string{
			"server": {
				"root_url": grafanaRouteURI,
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
	}, nil
}

func (r *GrafanaReconciler) createOrUpdateGrafanaResource(ctx context.Context, instance *grafoov1alpha1.Grafana, grafanaSpec grafanav1beta1.GrafanaSpec) error {
	operatedGrafana := &grafanav1beta1.Grafana{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		},
	}
	_, err := CreateOrUpdateWithRetries(ctx, r.Client, operatedGrafana, func() error {
		operatedGrafana.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "grafana")
		operatedGrafana.Spec = grafanaSpec
		return ctrl.SetControllerReference(instance, operatedGrafana, r.Scheme)
	})
	return err
}

func (r *GrafanaReconciler) createAuthReviewerResources(ctx context.Context, instance *grafoov1alpha1.Grafana, subjects []rbacv1.Subject) error {
	roleName := r.generateNameForComponent(instance, "auth-reviewer")
	ctrl.Log.Info("Creating auth reviewer resources", "roleName", roleName)
	rules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{"authorization.k8s.io"},
			Resources: []string{"subjectaccessreviews"},
			Verbs:     []string{"create"},
		},
		{
			APIGroups: []string{"authentication.k8s.io"},
			Resources: []string{"tokenreviews"},
			Verbs:     []string{"create"},
		},
	}
	if err := r.createClusterRole(ctx, instance, roleName, rules); err != nil {
		return err
	}
	return r.createClusterRoleBinding(ctx, instance, roleName, roleName, subjects)
}

func (r *GrafanaReconciler) createTempoStackResources(ctx context.Context, instance *grafoov1alpha1.Grafana, subjects []rbacv1.Subject) error {
	roleName := r.generateNameForComponent(instance, "tempostack-traces-reader")
	rules := []rbacv1.PolicyRule{
		{
			APIGroups:     []string{"tempo.grafana.com"},
			Resources:     []string{"dev"},
			ResourceNames: []string{"traces"},
			Verbs:         []string{"get"},
		},
	}
	if err := r.createClusterRole(ctx, instance, roleName, rules); err != nil {
		return err
	}
	return r.createClusterRoleBinding(ctx, instance, roleName, roleName, subjects)
}

func (r *GrafanaReconciler) createMonitoringViewBinding(ctx context.Context, instance *grafoov1alpha1.Grafana, subjects []rbacv1.Subject) error {
	bindingName := r.generateNameForComponent(instance, "cluster-monitoring-view")
	return r.createClusterRoleBinding(ctx, instance, bindingName, "cluster-monitoring-view", subjects)
}

func (r *GrafanaReconciler) createLoggingBindings(ctx context.Context, instance *grafoov1alpha1.Grafana, subjects []rbacv1.Subject) error {
	clusterRoles := []string{
		"cluster-logging-application-view",
		"cluster-logging-infrastructure-view",
		"cluster-logging-audit-view",
	}

	for _, roleName := range clusterRoles {
		bindingName := r.generateNameForComponent(instance, roleName)
		if err := r.createClusterRoleBindingIfRoleExists(ctx, instance, roleName, bindingName, subjects); err != nil {
			return err
		}
	}
	return nil
}

func (r *GrafanaReconciler) createClusterRole(ctx context.Context, instance *grafoov1alpha1.Grafana, name string, rules []rbacv1.PolicyRule) error {
	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	_, err := CreateOrUpdateWithRetries(ctx, r.Client, clusterRole, func() error {
		clusterRole.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "grafana")
		clusterRole.Rules = rules
		return nil
	})
	return err
}

func (r *GrafanaReconciler) createClusterRoleBinding(ctx context.Context, instance *grafoov1alpha1.Grafana, name, roleName string, subjects []rbacv1.Subject) error {
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	_, err := CreateOrUpdateWithRetries(ctx, r.Client, clusterRoleBinding, func() error {
		clusterRoleBinding.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "grafana")
		clusterRoleBinding.Subjects = subjects
		clusterRoleBinding.RoleRef = rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     roleName,
			APIGroup: "rbac.authorization.k8s.io",
		}
		return nil
	})
	return err
}

func (r *GrafanaReconciler) createClusterRoleBindingIfRoleExists(ctx context.Context, instance *grafoov1alpha1.Grafana, roleName, bindingName string, subjects []rbacv1.Subject) error {
	clusterRole := &rbacv1.ClusterRole{}
	err := r.Get(ctx, client.ObjectKey{Name: roleName}, clusterRole)
	if err == nil {
		return r.createClusterRoleBinding(ctx, instance, bindingName, roleName, subjects)
	}
	return nil
}

func (r *GrafanaReconciler) createSCCBinding(ctx context.Context, instance *grafoov1alpha1.Grafana, subjects []rbacv1.Subject) error {
	// Bind to the privileged SCC to allow NET_ADMIN for dsproxy
	// We use the system:openshift:scc:privileged ClusterRole which grants use of the privileged SCC
	// ClusterRoleBinding name must be unique across the cluster, so we include the namespace
	bindingName := fmt.Sprintf("%s-%s-%s", instance.Name, instance.Namespace, "scc-privileged")
	return r.createClusterRoleBinding(ctx, instance, bindingName, "system:openshift:scc:privileged", subjects)
}

func (r *GrafanaReconciler) createDSProxyPermissions(ctx context.Context, instance *grafoov1alpha1.Grafana, subjects []rbacv1.Subject) error {
	roleName := r.generateNameForComponent(instance, "dsproxy-role")
	rules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{"grafoo.cloudmonkey.org"},
			Resources: []string{"grafanadatasourcerules"},
			Verbs:     []string{"get", "list", "watch"},
		},
	}
	if err := r.createClusterRole(ctx, instance, roleName, rules); err != nil {
		return err
	}
	return r.createClusterRoleBinding(ctx, instance, roleName, roleName, subjects)
}
