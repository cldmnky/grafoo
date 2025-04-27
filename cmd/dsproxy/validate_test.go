package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	. "github.com/onsi/gomega"
)

func TestInitJWKSAndVerifyBearerToken(t *testing.T) {
	RegisterTestingT(t)

	// Generate a random 32-byte symmetric key for HS256
	key := make([]byte, 32)
	_, err := rand.Read(key)
	Expect(err).To(BeNil())
	encodedKey := base64.RawURLEncoding.EncodeToString(key)

	// Mock JWKS endpoint with a key that includes a "kid":"test"
	jwksJSON := fmt.Sprintf(`{"keys":[{"kty":"oct","k":"%s","kid":"test"}]}`, encodedKey)
	jwksSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, jwksJSON)
	}))
	defer jwksSrv.Close()

	// Mock OIDC discovery endpoint
	discoverySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]string{"jwks_uri": jwksSrv.URL}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer discoverySrv.Close()

	// Patch global jwksURL to point to our mock discovery endpoint
	origJWKSURL := f_jwksURL
	f_jwksURL = discoverySrv.URL
	defer func() { f_jwksURL = origJWKSURL }()

	// Call initJWKS and expect no error
	err = initJWKS()
	Expect(err).To(BeNil())
	Expect(jwks).ToNot(BeNil())

	// Create a valid JWT signed with the test key
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  "1234567890",
		"name": "John Doe",
		"iat":  time.Now().Unix(),
	})
	// Set the "kid" header to match the key in the JWKS
	token.Header["kid"] = "test"
	// Sign the token with the key
	tokenStr, err := token.SignedString(key)
	Expect(err).To(BeNil())

	// Create a request with the token
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	parsed, err := verifyBearerToken(req)
	Expect(err).To(BeNil())
	Expect(parsed).ToNot(BeNil())
	Expect(parsed.Valid).To(BeTrue())

	// Test missing Authorization header
	req2 := httptest.NewRequest("GET", "/", nil)
	_, err = verifyBearerToken(req2)
	Expect(err).ToNot(BeNil())

	// Test invalid token
	req3 := httptest.NewRequest("GET", "/", nil)
	req3.Header.Set("Authorization", "Bearer invalidtoken")
	_, err = verifyBearerToken(req3)
	Expect(err).ToNot(BeNil())

	// Test wrong prefix
	req4 := httptest.NewRequest("GET", "/", nil)
	req4.Header.Set("Authorization", "Token "+tokenStr)
	_, err = verifyBearerToken(req4)
	Expect(err).ToNot(BeNil())
}
