package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v2"
	"github.com/golang-jwt/jwt/v5"
)

var jwks *keyfunc.JWKS

func initJWKS() error {
	// Fetch from discovery URL with TLS verification disabled
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
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

	// Use keyfunc to get the key set, also with TLS verification disabled
	jwks, err = keyfunc.Get(config.JWKSURI, keyfunc.Options{
		RefreshInterval: 1 * time.Hour,
		Client: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
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
