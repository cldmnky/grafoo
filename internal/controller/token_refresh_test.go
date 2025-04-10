package controller

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
)

func TestNeedsRefreshedToken(t *testing.T) {
	tests := []struct {
		name   string
		status grafoov1alpha1.GrafanaStatus
		spec   grafoov1alpha1.GrafanaSpec
		want   bool
	}{
		{
			name: "nil token expiration and generation time",
			status: grafoov1alpha1.GrafanaStatus{
				TokenExpirationTime: nil,
				TokenGenerationTime: nil,
			},
			spec: grafoov1alpha1.GrafanaSpec{
				TokenDuration: &metav1.Duration{Duration: time.Hour},
			},
			want: true,
		},
		{
			name: "expired token - expiration in the past",
			status: grafoov1alpha1.GrafanaStatus{
				TokenExpirationTime: &metav1.Time{Time: time.Now().Add(-5 * time.Minute)},
				TokenGenerationTime: &metav1.Time{Time: time.Now().Add(-2 * time.Hour)},
			},
			spec: grafoov1alpha1.GrafanaSpec{
				TokenDuration: &metav1.Duration{Duration: time.Hour},
			},
			want: true,
		},
		{
			name: "token close to expiry - less than 15 min remaining",
			status: grafoov1alpha1.GrafanaStatus{
				TokenExpirationTime: &metav1.Time{Time: time.Now().Add(10 * time.Minute)},
				TokenGenerationTime: &metav1.Time{Time: time.Now()},
			},
			spec: grafoov1alpha1.GrafanaSpec{
				TokenDuration: &metav1.Duration{Duration: time.Hour},
			},
			want: true,
		},
		{
			name: "token not expired and enough time left",
			status: grafoov1alpha1.GrafanaStatus{
				TokenExpirationTime: &metav1.Time{Time: time.Now().Add(30 * time.Minute)},
				TokenGenerationTime: &metav1.Time{Time: time.Now().Add(-31 * time.Minute)},
			},
			spec: grafoov1alpha1.GrafanaSpec{
				TokenDuration: &metav1.Duration{Duration: time.Hour},
			},
			want: false,
		},
		{
			name: "token duration shorter than time between generation and expiry",
			status: grafoov1alpha1.GrafanaStatus{
				TokenExpirationTime: &metav1.Time{Time: time.Now().Add(30 * time.Minute)},
				TokenGenerationTime: &metav1.Time{Time: time.Now()},
			},
			spec: grafoov1alpha1.GrafanaSpec{
				TokenDuration: &metav1.Duration{Duration: 45 * time.Minute},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			graf := &grafoov1alpha1.Grafana{
				Status: tt.status,
				Spec:   tt.spec,
			}
			got, err := needsRefreshedToken(graf)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
