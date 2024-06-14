package controller

import (
	"context"
	"fmt"
	"net/url"

	"github.com/google/uuid"
	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
)

func (r *GrafanaReconciler) ReconcileDex(ctx context.Context, instance *grafoov1alpha1.Grafana) error {
	logger := log.FromContext(ctx)
	// Create a secrets for the client secret, once
	dexClientSecret := &corev1.Secret{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: r.generateNameForComponent(instance, "dex-client-secret"), Namespace: instance.Namespace}, dexClientSecret); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Creating dex client secret")
			dexClientSecret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      r.generateNameForComponent(instance, "dex-client-secret"),
					Namespace: instance.Namespace,
					Labels:    r.generateLabelsForComponent(instance, "dex"),
				},
				StringData: map[string]string{
					"clientSecret": uuid.New().String(),
				},
			}
			if err := r.Client.Create(ctx, dexClientSecret); err != nil {
				return err
			}
			// set the owner reference
			if err := ctrl.SetControllerReference(instance, dexClientSecret, r.Scheme); err != nil {
				return err
			}
		} else {
			return err
		}
	}
	// get the client secret again
	clientSecret, err := r.getClientSecret(ctx, instance)
	if err != nil {
		return err
	}

	dexRouteUri := r.generateRouteUriForComponent(ctx, instance, "dex")
	u, err := url.Parse(dexRouteUri)
	if err != nil {
		return err
	}
	dexRouteDomain := u.Hostname()

	grafanaRouteUri := r.generateRouteUriForComponent(ctx, instance, "grafana")

	// Create an SA for dex
	dexServiceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, "dex"),
			Namespace: instance.Namespace,
			Labels:    r.generateLabelsForComponent(instance, "dex"),
		},
	}
	_, err = CreateOrUpdateWithRetries(ctx, r.Client, dexServiceAccount, func() error {
		dexServiceAccount.Labels = r.generateLabelsForComponent(instance, "dex")
		dexServiceAccount.Annotations = map[string]string{
			"serviceaccounts.openshift.io/oauth-redirecturi.dex": dexRouteUri + "/callback",
		}
		return ctrl.SetControllerReference(instance, dexServiceAccount, r.Scheme)
	})
	if err != nil {
		return err
	}

	// create a new token for the dex service account
	request := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			Audiences:         nil,
			ExpirationSeconds: int64Ptr(int64(instance.Spec.TokenDuration.Duration.Seconds())),
		},
	}
	resp, err := r.Clientset.CoreV1().ServiceAccounts(instance.Namespace).CreateToken(ctx, dexServiceAccount.Name, request, metav1.CreateOptions{})
	if err != nil {
		return err
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
	// Create a secret for the dex config
	dexSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, "dex"),
			Namespace: instance.Namespace,
		},
	}
	// Get the sha for the secret
	configSha := sha256ForSecret(dexSecret.StringData["config.yaml"])

	op, err := CreateOrUpdateWithRetries(ctx, r.Client, dexSecret, func() error {
		dexSecret.Labels = r.generateLabelsForComponent(instance, "dex")
		dexSecret.StringData = dexConfig
		return ctrl.SetControllerReference(instance, dexSecret, r.Scheme)
	})
	if err != nil {
		return err
	}
	if op == controllerutil.OperationResultUpdated {
		logger.Info("Updated dex config secret", "sha", configSha)
	}

	// Create a dexDeployment instance for authentication
	dexDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, "dex"),
			Namespace: instance.Namespace,
		},
	}
	dwxDeploymentSpec := appsv1.DeploymentSpec{
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

	// CreateOrUpdate the dex instance
	_, err = CreateOrUpdateWithRetries(ctx, r.Client, dexDeployment, func() error {
		dexDeployment.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "dex")
		dexDeployment.Spec = dwxDeploymentSpec
		return ctrl.SetControllerReference(instance, dexDeployment, r.Scheme)
	})
	if err != nil {
		return err
	}

	// Create a service for dex
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
	// CreateOrUpdate the dex service
	_, err = CreateOrUpdateWithRetries(ctx, r.Client, dexService, func() error {
		dexService.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "dex")
		dexService.Spec = dexServiceSpec
		return ctrl.SetControllerReference(instance, dexService, r.Scheme)
	})
	if err != nil {
		return err
	}

	// Create an ingress for dex
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
										Name: dexService.Name,
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
	_, err = CreateOrUpdateWithRetries(ctx, r.Client, dexIngress, func() error {
		dexIngress.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "dex")
		dexIngress.ObjectMeta.Annotations = map[string]string{
			"route.openshift.io/termination": "edge",
		}
		dexIngress.Spec = dexIngressSpec
		return ctrl.SetControllerReference(instance, dexIngress, r.Scheme)
	})
	if err != nil {
		return err
	}

	return nil
}
