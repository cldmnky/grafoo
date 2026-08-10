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
)

var jwks *keyfunc.JWKS

// allowedSigningMethods restricts JWT verification to asymmetric RSA and ECDSA
// algorithms. Symmetric algorithms (HS*) are rejected because they would
// allow a token to be forged with the public key material.
var allowedSigningMethods = []string{
	"RS256", "RS384", "RS512",
	"ES256", "ES384", "ES512",
}

func getTLSConfig() (*tls.Config, error) {
	if f_caBundle == "" {
		return nil, nil // Use system certs
	}

	caCert, err := os.ReadFile(f_caBundle)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA bundle: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, errors.New("failed to append CA bundle to cert pool")
	}

	return &tls.Config{
		RootCAs: caCertPool,
	}, nil
}

// configureUpstreamTLS applies the CA bundle to the default HTTP transport so
// that the Prometheus reverse proxy verifies the upstream certificate. The
// reverse proxy from prom-label-proxy uses http.DefaultTransport when no
// transport is configured.
func configureUpstreamTLS() error {
	tlsConfig, err := getTLSConfig()
	if err != nil {
		return err
	}
	if tlsConfig == nil {
		return nil
	}
	http.DefaultTransport = &http.Transport{
		TLSClientConfig: tlsConfig,
	}
	return nil
}

func initJWKS() error {
	tlsConfig, err := getTLSConfig()
	if err != nil {
		return err
	}

	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	// Fetch from discovery URL
	res, err := httpClient.Get(f_jwksURL)
	if err != nil {
		return fmt.Errorf("failed to fetch OIDC discovery: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("OIDC discovery endpoint returned status %d", res.StatusCode)
	}

	var config struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(res.Body).Decode(&config); err != nil {
		return fmt.Errorf("failed to decode OIDC discovery response: %w", err)
	}
	if config.JWKSURI == "" {
		return errors.New("OIDC discovery response missing jwks_uri")
	}

	// Use keyfunc to get the key set
	jwks, err = keyfunc.Get(config.JWKSURI, keyfunc.Options{
		RefreshInterval: 1 * time.Hour,
		Client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		},
	})
	return err
}

func verifyBearerToken(r *http.Request) (*jwt.Token, error) {
	auth := r.Header.Get("Authorization")
	if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
		return nil, errors.New("missing bearer token")
	}

	tokenStr := strings.TrimPrefix(auth, "Bearer ")

	// Prevent nil pointer dereference if jwks is not initialized
	if jwks == nil {
		return nil, errors.New("jwks is not initialized")
	}

	parserOptions := []jwt.ParserOption{
		jwt.WithValidMethods(allowedSigningMethods),
		jwt.WithExpirationRequired(),
		// WithAudience accepts both string and array audience claims as long
		// as the configured audience is present.
		jwt.WithAudience(f_jwtAudience),
	}
	if f_jwtIssuer != "" {
		parserOptions = append(parserOptions, jwt.WithIssuer(f_jwtIssuer))
	}

	// Validate the token
	token, err := jwt.Parse(tokenStr, jwks.Keyfunc, parserOptions...)
	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, errors.New("invalid token")
	}

	return token, nil
}
