package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	. "github.com/onsi/gomega"
)

// Note: proxyHandler tests removed as we now use prom-label-proxy integration
// which provides its own proxy implementation with label injection.
// The main functionality is tested through integration tests.

func TestAuthMiddleware_SuccessAndFailure(t *testing.T) {
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

	// Call initAuth and expect no error
	err = initAuth()
	Expect(err).To(BeNil())
	Expect(jwks).ToNot(BeNil())

	// Create a valid JWT signed with the test key
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":            "1234567890",
		"name":           "John Doe",
		"iat":            time.Now().Unix(),
		"aud":            "example-app",
		"iss":            "https://example.com",
		"exp":            time.Now().Add(time.Hour).Unix(),
		"email":          "kube:admin",
		"email_verified": false,
		"groups":         []string{"system:authenticated", "system:cluster-admins"},
	})
	token.Header["kid"] = "test"
	tokenStr, err := token.SignedString(key)
	Expect(err).To(BeNil())

	// Handler to wrap
	finalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "OK")
		// Get the context - we now use 'sub' as the primary identity
		email := r.Context().Value(ContextKeyEmail)
		Expect(email).To(Equal("1234567890")) // This is the 'sub' claim now
		groups := r.Context().Value(ContextKeyGroups)
		Expect(groups).ToNot(BeNil())
		Expect(groups).To(BeAssignableToTypeOf([]string{}))
		Expect(groups).To(Equal([]string{"system:authenticated", "system:cluster-admins"}))
	})

	// Compose middleware
	handler := authMiddleware(finalHandler)

	// --- Test: Valid token ---
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	Expect(w.Code).To(Equal(http.StatusOK))
	Expect(w.Body.String()).To(Equal("OK"))

	// --- Test: Missing token ---
	req2 := httptest.NewRequest("GET", "/", nil)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	Expect(w2.Code).To(Equal(http.StatusUnauthorized))

	// --- Test: Invalid token ---
	req3 := httptest.NewRequest("GET", "/", nil)
	req3.Header.Set("Authorization", "Bearer invalidtoken")
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)
	Expect(w3.Code).To(Equal(http.StatusUnauthorized))

	// --- Test: Wrong prefix ---
	req4 := httptest.NewRequest("GET", "/", nil)
	req4.Header.Set("Authorization", "Token "+tokenStr)
	w4 := httptest.NewRecorder()
	handler.ServeHTTP(w4, req4)
	Expect(w4.Code).To(Equal(http.StatusUnauthorized))
}
