package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	. "github.com/onsi/gomega"
)

// mockTargetServer returns a test server that echoes request details.
func mockTargetServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

func TestProxyHandler_GET(t *testing.T) {
	target := mockTargetServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test-Header", "test-value")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("proxied response"))
	})
	defer target.Close()

	// Extract host and path from target URL
	targetURL := target.URL
	parts := strings.SplitN(strings.TrimPrefix(targetURL, "http://"), "/", 2)
	host := parts[0]
	path := "/testpath"

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host

	rr := httptest.NewRecorder()
	handler := proxyHandler()

	// Patch the request URL to match the target
	req.URL.Scheme = "http"
	req.URL.Host = host

	handler.ServeHTTP(rr, req)

	resp := rr.Result()
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Test-Header"); got != "test-value" {
		t.Errorf("expected X-Test-Header 'test-value', got '%s'", got)
	}
	if !bytes.Contains(body, []byte("proxied response")) {
		t.Errorf("expected body to contain 'proxied response', got '%s'", string(body))
	}
}

func TestProxyHandler_POST(t *testing.T) {
	target := mockTargetServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("echo: " + string(b)))
	})
	defer target.Close()

	host := strings.TrimPrefix(target.URL, "http://")
	reqBody := "hello world"
	req := httptest.NewRequest(http.MethodPost, "/post", bytes.NewBufferString(reqBody))
	req.Host = host
	req.Header.Set("Content-Type", "text/plain")

	rr := httptest.NewRecorder()
	handler := proxyHandler()

	req.URL.Scheme = "http"
	req.URL.Host = host

	handler.ServeHTTP(rr, req)

	resp := rr.Result()
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected status 201, got %d", resp.StatusCode)
	}
	if !bytes.Contains(body, []byte("echo: "+reqBody)) {
		t.Errorf("expected body to contain 'echo: %s', got '%s'", reqBody, string(body))
	}
}

func TestProxyHandler_TargetError(t *testing.T) {
	// Use an invalid host to force an error
	req := httptest.NewRequest(http.MethodGet, "/fail", nil)
	req.Host = "invalid.host.local"

	rr := httptest.NewRecorder()
	handler := proxyHandler()
	handler.ServeHTTP(rr, req)

	resp := rr.Result()
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("expected status 502, got %d", resp.StatusCode)
	}
	if !bytes.Contains(body, []byte("Proxy error")) {
		t.Errorf("expected body to contain 'Proxy error', got '%s'", string(body))
	}
}

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

	// Call initJWKS and expect no error
	err = initJWKS()
	Expect(err).To(BeNil())
	Expect(jwks).ToNot(BeNil())

	// Create a valid JWT signed with the test key
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":            "1234567890",
		"name":           "John Doe",
		"iat":            time.Now().Unix(),
		"aud":            "grafana",
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
		// Get the context
		email := r.Context().Value(ContextKeyEmail)
		Expect(email).To(Equal("kube:admin"))
		groups := r.Context().Value(ContextKeyGroups)
		Expect(groups).ToNot(BeNil())
		Expect(groups).To(BeAssignableToTypeOf([]string{}))
		Expect(groups).To(Equal([]string{"system:authenticated", "system:cluster-admins"}))
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
