package controller

import (
	"context"

	configv1 "github.com/openshift/api/config/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
)

func (r *GrafanaReconciler) ReconcileDex(ctx context.Context, instance *grafoov1alpha1.Grafana) error {
	logger := log.FromContext(ctx)
	// get the ingress domain for the cluster
	ingressConfig := &configv1.Ingress{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: "cluster"}, ingressConfig); err != nil {
		return err
	}
	logger.Info("dex", "ingressDomain", ingressConfig.Spec.Domain)
	// Create a secret for the dex config
	dexSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name + "-dex",
			Namespace: instance.Namespace,
		},
		StringData: map[string]string{
			"config.yaml": `
logger:
  level: debug
connectors:
- config:
    clientID: system:serviceaccount:` + instance.Namespace + `:dex
    clientSecret: someclientsecret
    insecureCA: true
    issuer: https://kubernetes.default.svc
    redirectURI: https://dex.` + instance.Namespace + `.` + ingressConfig.Spec.Domain + `/callback
  id: openshift
  name: OpenShift
  type: openshift
grpc:
  addr: 0.0.0.0:5557
  issuer: https://dex.` + instance.Namespace + `.` + ingressConfig.Spec.Domain + `
  oauth2:
    skipApprovalScreen: true
staticClients:
- id: grafana
  name: Grafana
  redirectURIs: []
  secret: somerandomsecret
storage:
  type: memory
telemetry:
  http: 0.0.0.0:5558
web:
  http: 0.0.0.0:5556	
`,
		},
	}
	op, err := CreateOrUpdateWithRetries(ctx, r.Client, dexSecret, func() error {
		return ctrl.SetControllerReference(instance, dexSecret, r.Scheme)
	})
	if err != nil {
		return err
	}

	// Create a dexDeployment instance for authentication
	dexDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name + "-dex",
			Namespace: instance.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": instance.Name + "-dex",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": instance.Name + "-dex",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "dex",
							Image: "dexidp/dex:v2.39.1-distroless",
						},
					},
				},
			},
		},
	}

	// Set Grafana instance as the owner and controller
	if err := ctrl.SetControllerReference(instance, dexDeployment, r.Scheme); err != nil {
		return err
	}

	// CreateOrUpdate the dex instance
	op, err = CreateOrUpdateWithRetries(ctx, r.Client, dexDeployment, func() error {
		return ctrl.SetControllerReference(instance, dexDeployment, r.Scheme)
	})
	if err != nil {
		return err
	}
	logger.Info("dex", "op", op)

	return nil
}
