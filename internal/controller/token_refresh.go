package controller

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
)

func needsRefreshedToken(instance *grafoov1alpha1.Grafana) (bool, error) {
	// Check if the token needs to be refreshed
	if instance.Status.TokenExpirationTime != nil {
		if instance.Status.TokenExpirationTime.Before(&metav1.Time{
			Time: time.Now(),
		}) {
			return true, nil
		}
		// If the token is not expired, check if it needs to be refreshed
		//lint:ignore S1024 metav1.Time is not comparable
		if instance.Status.TokenExpirationTime.Sub(time.Now()) < time.Minute*15 {
			return true, nil
		}
	}
	if instance.Status.TokenGenerationTime == nil {
		return true, nil
	}
	if time.Duration(time.Duration(instance.Status.TokenExpirationTime.Time.Sub(instance.Status.TokenGenerationTime.Time)).Seconds()) < time.Duration(instance.Spec.TokenDuration.Duration.Seconds()) {
		return true, nil
	}
	return false, nil
}
