package controller

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
)

func needsRefreshedToken(instance *grafoov1alpha1.Grafana) (bool, error) {
	// Check if the token needs to be refreshed

	// If either time is missing, we need to generate a token
	if instance.Status.TokenExpirationTime == nil || instance.Status.TokenGenerationTime == nil {
		return true, nil
	}

	// If the token is already expired
	if instance.Status.TokenExpirationTime.Before(&metav1.Time{
		Time: time.Now(),
	}) {
		return true, nil
	}

	// If the token is close to expiry (less than 15 minutes remaining), refresh proactively
	//lint:ignore S1024 metav1.Time is not comparable
	if instance.Status.TokenExpirationTime.Sub(time.Now()) < time.Minute*15 {
		return true, nil
	}

	// If the actual token lifetime is shorter than the configured duration,
	// the token may have been truncated and should be refreshed
	actualTokenLifetime := instance.Status.TokenExpirationTime.Time.Sub(instance.Status.TokenGenerationTime.Time)
	if actualTokenLifetime < instance.Spec.TokenDuration.Duration {
		return true, nil
	}

	return false, nil
}
