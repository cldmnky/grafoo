package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v2"
	"github.com/golang-jwt/jwt/v5"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	jwks       *keyfunc.JWKS
	kubeClient *kubernetes.Clientset
)

func getTLSConfig() (*tls.Config, error) {
	var caCertPool *x509.CertPool
	if f_caBundle != "" {
		caCert, err := os.ReadFile(f_caBundle)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA bundle: %w", err)
		}

		caCertPool = x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, errors.New("failed to append CA bundle to cert pool")
		}
	}

	if caCertPool == nil && !f_insecureSkipVerify {
		return nil, nil // Use system certs and default verification
	}

	return &tls.Config{
		RootCAs:            caCertPool,
		InsecureSkipVerify: f_insecureSkipVerify,
	}, nil
}

func initAuth() error {
	if f_tokenReview {
		config, err := rest.InClusterConfig()
		if err != nil {
			return fmt.Errorf("failed to get in-cluster config: %w", err)
		}
		kubeClient, err = kubernetes.NewForConfig(config)
		if err != nil {
			return fmt.Errorf("failed to create kube client: %w", err)
		}
	}

	tlsConfig, err := getTLSConfig()
	if err != nil {
		return err
	}

	// Fetch from discovery URL
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}
	res, err := client.Get(f_jwksURL)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	var config struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(res.Body).Decode(&config); err != nil {
		return err
	}

	// Use keyfunc to get the key set
	jwks, err = keyfunc.Get(config.JWKSURI, keyfunc.Options{
		RefreshInterval: 1 * time.Hour,
		Client: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		},
	})
	return err
}

func verifyBearerToken(r *http.Request) (*jwt.Token, error) {
	var tokenStr string
	var isIdToken bool

	// Check X-Id-Token first
	if idToken := r.Header.Get("X-Id-Token"); idToken != "" {
		tokenStr = idToken
		isIdToken = true
	} else {
		auth := r.Header.Get("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			return nil, errors.New("missing bearer token")
		}
		tokenStr = strings.TrimPrefix(auth, "Bearer ")
	}

	if f_tokenReview && !isIdToken {
		if kubeClient == nil {
			return nil, errors.New("kube client is not initialized")
		}
		tr := &authenticationv1.TokenReview{
			Spec: authenticationv1.TokenReviewSpec{
				Token: tokenStr,
			},
		}
		if f_jwtAudience != "" {
			tr.Spec.Audiences = []string{f_jwtAudience}
		}

		result, err := kubeClient.AuthenticationV1().TokenReviews().Create(r.Context(), tr, metav1.CreateOptions{})
		if err != nil {
			return nil, fmt.Errorf("token review failed: %w", err)
		}
		if !result.Status.Authenticated {
			return nil, fmt.Errorf("token invalid: %s", result.Status.Error)
		}

		// Construct a fake jwt.Token with claims from TokenReview
		claims := jwt.MapClaims{
			"sub":    result.Status.User.Username,
			"groups": result.Status.User.Groups,
			"aud":    f_jwtAudience, // We validated it via TokenReview (if supported) or we trust it
		}

		return &jwt.Token{
			Valid:  true,
			Claims: claims,
		}, nil
	}

	// Prevent nil pointer dereference if jwks is not initialized
	if jwks == nil {
		return nil, errors.New("jwks is not initialized")
	}

	// Validate the token
	token, err := jwt.Parse(tokenStr, jwks.Keyfunc)
	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, errors.New("invalid token")
	}

	return token, nil
}
