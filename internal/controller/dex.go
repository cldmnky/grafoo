package controller

import (
	"context"
	"fmt"
	"net/url"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
)

func (r *GrafanaReconciler) ReconcileDex(ctx context.Context, instance *grafoov1alpha1.Grafana, needsRefresh bool) error {
	logger := log.FromContext(ctx)

	if !instance.Spec.Dex.Enabled {
		logger.Info("Dex is not enabled, skipping reconciliation")
		// If Dex is not enabled, we should remove the Dex resources
		if err := r.removeDexResources(ctx, instance); err != nil {
			logger.Error(err, "Failed to remove Dex resources")
		}
		return nil
	}

	dexRouteUri, dexRouteDomain, grafanaRouteUri, err := r.getRouteUris(ctx, instance)
	if err != nil {
		return err
	}

	dexServiceAccount, err := r.reconcileDexServiceAccount(ctx, instance, dexRouteUri)
	if err != nil {
		return err
	}

	boundTokenSecret, err := r.reconcileBoundTokenSecret(ctx, instance, dexServiceAccount)
	if err != nil {
		return err
	}

	clientSecret, err := r.ensureDexClientSecret(ctx, instance, logger)
	if err != nil {
		return err
	}

	var configSha string
	if needsRefresh {
		logger.Info("Refreshing Dex config secret")
		configSha, err = r.reconcileDexConfigSecret(ctx, instance, dexRouteUri, grafanaRouteUri, boundTokenSecret, clientSecret, logger)
		if err != nil {
			return err
		}
	}

	if configSha != "" {
		err = r.reconcileDexDeployment(ctx, instance, dexServiceAccount, configSha)
		if err != nil {
			return err
		}
	}

	err = r.reconcileDexService(ctx, instance)
	if err != nil {
		return err
	}

	err = r.reconcileDexIngress(ctx, instance, dexRouteDomain)
	if err != nil {
		return err
	}

	return nil
}

func (r *GrafanaReconciler) removeDexResources(ctx context.Context, instance *grafoov1alpha1.Grafana) error {
	logger := log.FromContext(ctx)
	dexServiceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, "dex"),
			Namespace: instance.Namespace,
		},
	}
	if err := r.Client.Delete(ctx, dexServiceAccount); err != nil && !apierrors.IsNotFound(err) {
		logger.Error(err, "Failed to delete Dex service account")
	}
	dexSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, "dex"),
			Namespace: instance.Namespace,
		},
	}
	if err := r.Client.Delete(ctx, dexSecret); err != nil && !apierrors.IsNotFound(err) {
		logger.Error(err, "Failed to delete Dex secret")
	}
	dexClientSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, "dex-client-secret"),
			Namespace: instance.Namespace,
		},
	}
	if err := r.Client.Delete(ctx, dexClientSecret); err != nil && !apierrors.IsNotFound(err) {
		logger.Error(err, "Failed to delete Dex client secret")
	}
	boundTokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-dex-token", instance.Name),
			Namespace: instance.Namespace,
		},
	}
	if err := r.Client.Delete(ctx, boundTokenSecret); err != nil && !apierrors.IsNotFound(err) {
		logger.Error(err, "Failed to delete Dex bound token secret")
	}
	dexDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, "dex"),
			Namespace: instance.Namespace,
		},
	}
	if err := r.Client.Delete(ctx, dexDeployment); err != nil && !apierrors.IsNotFound(err) {
		logger.Error(err, "Failed to delete Dex deployment")
	}
	dexService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, "dex"),
			Namespace: instance.Namespace,
		},
	}
	if err := r.Client.Delete(ctx, dexService); err != nil && !apierrors.IsNotFound(err) {
		logger.Error(err, "Failed to delete Dex service")
	}
	dexIngress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, "dex"),
			Namespace: instance.Namespace,
		},
	}
	if err := r.Client.Delete(ctx, dexIngress); err != nil && !apierrors.IsNotFound(err) {
		logger.Error(err, "Failed to delete Dex ingress")
	}
	return nil
}

func (r *GrafanaReconciler) getRouteUris(ctx context.Context, instance *grafoov1alpha1.Grafana) (string, string, string, error) {
	dexRouteUri := r.generateRouteUriForComponent(ctx, instance, "dex")
	u, err := url.Parse(dexRouteUri)
	if err != nil {
		return "", "", "", err
	}
	dexRouteDomain := u.Hostname()
	grafanaRouteUri := r.generateRouteUriForComponent(ctx, instance, "grafana")
	return dexRouteUri, dexRouteDomain, grafanaRouteUri, nil
}

func (r *GrafanaReconciler) reconcileDexServiceAccount(ctx context.Context, instance *grafoov1alpha1.Grafana, dexRouteUri string) (*corev1.ServiceAccount, error) {
	dexServiceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, "dex"),
			Namespace: instance.Namespace,
			Labels:    r.generateLabelsForComponent(instance, "dex"),
		},
	}
	_, err := CreateOrUpdateWithRetries(ctx, r.Client, dexServiceAccount, func() error {
		dexServiceAccount.Labels = r.generateLabelsForComponent(instance, "dex")
		dexServiceAccount.Annotations = map[string]string{
			"serviceaccounts.openshift.io/oauth-redirecturi.dex": dexRouteUri + "/callback",
		}
		return ctrl.SetControllerReference(instance, dexServiceAccount, r.Scheme)
	})
	return dexServiceAccount, err
}

func (r *GrafanaReconciler) reconcileBoundTokenSecret(ctx context.Context, instance *grafoov1alpha1.Grafana, dexServiceAccount *corev1.ServiceAccount) (*corev1.Secret, error) {
	boundTokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-dex-token", instance.Name),
			Namespace: instance.Namespace,
			Annotations: map[string]string{
				"kubernetes.io/service-account.name": dexServiceAccount.Name,
			},
			Labels: r.generateLabelsForComponent(instance, "dex"),
		},
		Type: corev1.SecretTypeServiceAccountToken,
	}
	_, err := CreateOrUpdateWithRetries(ctx, r.Client, boundTokenSecret, func() error {
		boundTokenSecret.Labels = r.generateLabelsForComponent(instance, "dex")
		return ctrl.SetControllerReference(instance, boundTokenSecret, r.Scheme)
	})
	return boundTokenSecret, err
}

func (r *GrafanaReconciler) ensureDexClientSecret(ctx context.Context, instance *grafoov1alpha1.Grafana, logger logr.Logger) (string, error) {
	clientSecret, err := r.getClientSecret(ctx, instance)
	if err != nil && apierrors.IsNotFound(err) {
		logger.Info("Creating dex client secret")
		dexClientSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      r.generateNameForComponent(instance, "dex-client-secret"),
				Namespace: instance.Namespace,
				Labels:    r.generateLabelsForComponent(instance, "dex"),
			},
			StringData: map[string]string{
				"clientSecret": uuid.New().String(),
			},
		}
		if err := ctrl.SetControllerReference(instance, dexClientSecret, r.Scheme); err != nil {
			return "", err
		}
		if err := r.Client.Create(ctx, dexClientSecret); err != nil {
			return "", err
		}
		clientSecret = dexClientSecret.StringData["clientSecret"]
	}
	return clientSecret, err
}

// reconcileDexConfigSecret creates or updates the Dex config secret with the provided configuration.
// It generates a SHA256 hash of the config and sets it as an annotation on the secret.
// It also creates a token request for the bound token secret and includes it in the config.
// The config is passed as a string in the format expected by Dex.
// The function returns the SHA256 hash of the config.
func (r *GrafanaReconciler) reconcileDexConfigSecret(ctx context.Context, instance *grafoov1alpha1.Grafana, dexRouteUri, grafanaRouteUri string, boundTokenSecret *corev1.Secret, clientSecret string, logger logr.Logger) (string, error) {
	request := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			Audiences: nil,
			BoundObjectRef: &authenticationv1.BoundObjectReference{
				Kind:       "Secret",
				Name:       boundTokenSecret.Name,
				UID:        boundTokenSecret.UID,
				APIVersion: "v1",
			},
			ExpirationSeconds: int64Ptr(int64(instance.Spec.TokenDuration.Seconds())),
		},
	}
	resp, err := r.Clientset.CoreV1().ServiceAccounts(instance.Namespace).CreateToken(ctx, boundTokenSecret.Annotations["kubernetes.io/service-account.name"], request, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}
	logger.Info("Created token for dex service account", "token expiration", resp.Status.ExpirationTimestamp.Time)

	saToken := resp.Status.Token

	dexConfig := map[string]string{
		"config.yaml": fmt.Sprintf(`
logger:
  level: debug
connectors:
- config:
    clientID: system:serviceaccount:%s:%s-dex
    clientSecret: %s
    insecureCA: true
    issuer: https://kubernetes.default.svc
    redirectURI: %s/callback
  id: openshift
  name: OpenShift
  type: openshift
grpc:
  addr: 0.0.0.0:5557
issuer: %s
oauth2:
  skipApprovalScreen: true
staticClients:
- id: grafana
  name: Grafana
  redirectURIs:
  - '%s/login/generic_oauth'
  secret: %s
storage:
  type: memory
telemetry:
  http: 0.0.0.0:5558
web:
  http: 0.0.0.0:5556	
`, instance.Namespace, instance.Name, saToken, dexRouteUri, dexRouteUri, grafanaRouteUri, clientSecret),
	}

	dexSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, "dex"),
			Namespace: instance.Namespace,
		},
	}

	configSha := sha256ForSecret(dexConfig["config.yaml"])

	op, err := CreateOrUpdateWithRetries(ctx, r.Client, dexSecret, func() error {
		dexSecret.Labels = r.generateLabelsForComponent(instance, "dex")
		dexSecret.Annotations = map[string]string{
			"checksum/config.yaml": configSha,
		}
		dexSecret.StringData = dexConfig
		return ctrl.SetControllerReference(instance, dexSecret, r.Scheme)
	})
	if err != nil {
		return "", err
	}
	if op == controllerutil.OperationResultUpdated {
		logger.Info("Updated dex config secret", "sha", configSha)
	}
	return configSha, nil
}

func (r *GrafanaReconciler) reconcileDexDeployment(ctx context.Context, instance *grafoov1alpha1.Grafana, dexServiceAccount *corev1.ServiceAccount, configSha string) error {
	dexDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, "dex"),
			Namespace: instance.Namespace,
		},
	}
	dexDeploymentSpec := appsv1.DeploymentSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: r.generateLabelsForComponent(instance, "dex"),
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: r.generateLabelsForComponent(instance, "dex"),
				Annotations: map[string]string{
					"checksum/config.yaml": configSha,
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "dex",
						Image: grafoov1alpha1.DexImage,
						Args: []string{
							"dex",
							"serve",
							"--web-http-addr",
							fmt.Sprintf("0.0.0.0:%d", grafoov1alpha1.DexHttpPort),
							"--grpc-addr",
							fmt.Sprintf("0.0.0.0:%d", grafoov1alpha1.DexGrpcPort),
							"--telemetry-addr",
							fmt.Sprintf("0.0.0.0:%d", grafoov1alpha1.DexMetricsPort),
							"/config/config.yaml",
						},
						Ports: []corev1.ContainerPort{
							{
								ContainerPort: grafoov1alpha1.DexHttpPort,
								Name:          "http",
							}, {
								ContainerPort: grafoov1alpha1.DexGrpcPort,
								Name:          "grpc",
							}, {
								ContainerPort: grafoov1alpha1.DexMetricsPort,
								Name:          "metrics",
							},
						},
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: boolPtr(false),
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{
									"ALL",
								},
							},
							RunAsNonRoot: boolPtr(true),
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "config-volume",
							MountPath: "/config",
						}},
					},
				},
				ServiceAccountName: dexServiceAccount.Name,
				Volumes: []corev1.Volume{
					{
						Name: "config-volume",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: r.generateNameForComponent(instance, "dex"),
							},
						},
					},
				},
			},
		},
	}

	_, err := CreateOrUpdateWithRetries(ctx, r.Client, dexDeployment, func() error {
		dexDeployment.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "dex")
		dexDeployment.Spec.Replicas = int32Ptr(1)
		dexDeployment.Spec = dexDeploymentSpec
		dexDeployment.Spec.Template.ObjectMeta.Annotations = map[string]string{
			"checksum/config.yaml": configSha,
		}
		return ctrl.SetControllerReference(instance, dexDeployment, r.Scheme)
	})
	return err
}

func (r *GrafanaReconciler) reconcileDexService(ctx context.Context, instance *grafoov1alpha1.Grafana) error {
	dexService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, "dex"),
			Namespace: instance.Namespace,
		},
	}
	dexServiceSpec := corev1.ServiceSpec{
		Ports: []corev1.ServicePort{
			{
				Name:       "http",
				Port:       grafoov1alpha1.DexHttpPort,
				TargetPort: intstr.FromInt(int(grafoov1alpha1.DexHttpPort)),
			},
			{
				Name:       "grpc",
				Port:       grafoov1alpha1.DexGrpcPort,
				TargetPort: intstr.FromInt(int(grafoov1alpha1.DexGrpcPort)),
			},
			{
				Name:       "metrics",
				Port:       grafoov1alpha1.DexMetricsPort,
				TargetPort: intstr.FromInt(int(grafoov1alpha1.DexMetricsPort)),
			},
		},
		Selector: r.generateLabelsForComponent(instance, "dex"),
	}
	_, err := CreateOrUpdateWithRetries(ctx, r.Client, dexService, func() error {
		dexService.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "dex")
		dexService.Spec = dexServiceSpec
		return ctrl.SetControllerReference(instance, dexService, r.Scheme)
	})
	return err
}

func (r *GrafanaReconciler) reconcileDexIngress(ctx context.Context, instance *grafoov1alpha1.Grafana, dexRouteDomain string) error {
	dexIngress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, "dex"),
			Namespace: instance.Namespace,
		},
	}
	dexIngressSpec := networkingv1.IngressSpec{
		Rules: []networkingv1.IngressRule{
			{
				Host: dexRouteDomain,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{
							{
								Path: "",
								PathType: func() *networkingv1.PathType {
									pt := networkingv1.PathTypeImplementationSpecific
									return &pt
								}(),
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: r.generateNameForComponent(instance, "dex"),
										Port: networkingv1.ServiceBackendPort{
											Number: grafoov1alpha1.DexHttpPort,
										},
									},
								},
							},
						},
					},
				},
			},
		},
		TLS: []networkingv1.IngressTLS{
			{},
		},
	}
	_, err := CreateOrUpdateWithRetries(ctx, r.Client, dexIngress, func() error {
		dexIngress.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "dex")
		dexIngress.ObjectMeta.Annotations = map[string]string{
			"route.openshift.io/termination": "edge",
		}
		dexIngress.Spec = dexIngressSpec
		return ctrl.SetControllerReference(instance, dexIngress, r.Scheme)
	})
	return err
}
