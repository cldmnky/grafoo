package controller

import (
	"context"
	"crypto/sha256"
	"fmt"

	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
)

// CreateOrUpdateWithRetries creates or updates the given object in the Kubernetes with retries
func CreateOrUpdateWithRetries(
	ctx context.Context,
	c client.Client,
	obj client.Object,
	f controllerutil.MutateFn,
) (controllerutil.OperationResult, error) {
	var operationResult controllerutil.OperationResult
	updateErr := wait.ExponentialBackoff(retry.DefaultBackoff, func() (ok bool, err error) {
		operationResult, err = controllerutil.CreateOrUpdate(ctx, c, obj, f)
		if err == nil {
			return true, nil
		}
		if !apierrors.IsConflict(err) {
			return false, err
		}
		return false, nil
	})
	return operationResult, updateErr
}

// boolPtr returns a pointer to val
func boolPtr(val bool) *bool {
	return &val
}

func int64Ptr(val int64) *int64 {
	return &val
}

// generateNameForComponent generates a name for the Grafana instance components
func (r *GrafanaReconciler) generateNameForComponent(instance *grafoov1alpha1.Grafana, component string) string {
	return fmt.Sprintf("%s-%s", instance.Name, component)
}

// generateLabelsForComponent generates labels for the Grafana instance components
func (r *GrafanaReconciler) generateLabelsForComponent(instance *grafoov1alpha1.Grafana, component string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "grafana",
		"app.kubernetes.io/instance":  instance.Name,
		"app.kubernetes.io/component": component,
	}
}

// generateRouteUriForComponent generates a route URI for the Grafana instance components
func (r *GrafanaReconciler) generateRouteUriForComponent(ctx context.Context, instance *grafoov1alpha1.Grafana, component string) string {
	logger := log.FromContext(ctx)
	var ingressDomain string
	if instance.Spec.IngressDomain != "" {
		ingressDomain = instance.Spec.IngressDomain
		return fmt.Sprintf("https://%s.%s", component, ingressDomain)
	}
	ingressConfig := &configv1.Ingress{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: "cluster"}, ingressConfig); err != nil {
		logger.Error(err, "Failed to get cluster ingress")
		ingressDomain = "apps.foo.bar"
	} else {
		ingressDomain = ingressConfig.Spec.Domain
	}
	return fmt.Sprintf("https://%s-%s-%s.%s", instance.Name, component, instance.Namespace, ingressDomain)
}

// sha256ForSecret
func sha256ForSecret(data string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(data)))
}

// getClientSecret returns the client secret for the given instance
func (r *GrafanaReconciler) getClientSecret(ctx context.Context, instance *grafoov1alpha1.Grafana) (string, error) {
	secret := &corev1.Secret{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: r.generateNameForComponent(instance, "dex-client-secret"), Namespace: instance.Namespace}, secret); err != nil {
		return "", err
	}
	if secret.Data == nil {
		return "", fmt.Errorf("secret data is empty")
	}
	if _, ok := secret.Data["clientSecret"]; !ok {
		return "", fmt.Errorf("clientSecret key not found in secret")
	}
	return string(secret.Data["clientSecret"]), nil
}
