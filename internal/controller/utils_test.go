package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
)

// Test_getClientSecret tests the getClientSecret function in utils.go
func Test_getClientSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = grafoov1alpha1.AddToScheme(scheme)

	ctx := context.TODO()

	// Create an instance of Grafana
	instance := &grafoov1alpha1.Grafana{
		Spec: grafoov1alpha1.GrafanaSpec{},
	}
	instance.Name = "test-grafana"
	instance.Namespace = "test-namespace"

	t.Run("Secret found with valid clientSecret", func(t *testing.T) {
		// Create a secret containing the clientSecret key
		secret := &corev1.Secret{
			Data: map[string][]byte{"clientSecret": []byte("my-secret")},
		}
		secret.Name = "test-grafana-dex-client-secret"
		secret.Namespace = "test-namespace"

		// Create a fake client and populate it with the secret
		fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

		r := &GrafanaReconciler{Client: fakeClient}

		val, err := r.getClientSecret(ctx, instance)
		assert.NoError(t, err)
		assert.Equal(t, "my-secret", val)
	})

	t.Run("Secret not found", func(t *testing.T) {
		fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).Build()
		r := &GrafanaReconciler{Client: fakeClient}

		val, err := r.getClientSecret(ctx, instance)
		assert.Empty(t, val)
		assert.Error(t, err)
	})

	t.Run("Secret found but data is nil", func(t *testing.T) {
		secret := &corev1.Secret{}
		secret.Name = "test-grafana-dex-client-secret"
		secret.Namespace = "test-namespace"

		fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
		r := &GrafanaReconciler{Client: fakeClient}

		val, err := r.getClientSecret(ctx, instance)
		assert.Empty(t, val)
		assert.EqualError(t, err, "secret data is empty")
	})

	t.Run("Secret found but clientSecret key is missing", func(t *testing.T) {
		secret := &corev1.Secret{
			Data: map[string][]byte{"somethingElse": []byte("abc")},
		}
		secret.Name = "test-grafana-dex-client-secret"
		secret.Namespace = "test-namespace"

		fakeClient := clientfake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
		r := &GrafanaReconciler{Client: fakeClient}

		val, err := r.getClientSecret(ctx, instance)
		assert.Empty(t, val)
		assert.EqualError(t, err, "clientSecret key not found in secret")
	})
}

// FakeClient is a minimal interface to match the usage in getClientSecret.
type FakeClient interface {
	Get(ctx context.Context, key types.NamespacedName, obj runtime.Object) error
}
