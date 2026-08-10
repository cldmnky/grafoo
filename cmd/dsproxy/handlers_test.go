package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/onsi/gomega"
)

// The authMiddleware is tested end-to-end with a real RSA-signed token so the
// full chain (JWKS -> signature -> audience -> expiration -> sub extraction)
// is exercised.
func TestAuthMiddleware_SuccessAndFailure(t *testing.T) {
	RegisterTestingT(t)
	env := setupJWKS(t)

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

	handler := authMiddleware(finalHandler)

	// --- Test: Valid token ---
	tokenStr := env.signToken(t, defaultClaims())
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

	// --- Test: Token without exp ---
	claims := defaultClaims()
	delete(claims, "exp")
	req5 := httptest.NewRequest("GET", "/", nil)
	req5.Header.Set("Authorization", "Bearer "+env.signToken(t, claims))
	w5 := httptest.NewRecorder()
	handler.ServeHTTP(w5, req5)
	Expect(w5.Code).To(Equal(http.StatusUnauthorized))

	// --- Test: Wrong audience ---
	claims = defaultClaims()
	claims["aud"] = "wrong-audience"
	req6 := httptest.NewRequest("GET", "/", nil)
	req6.Header.Set("Authorization", "Bearer "+env.signToken(t, claims))
	w6 := httptest.NewRecorder()
	handler.ServeHTTP(w6, req6)
	Expect(w6.Code).To(Equal(http.StatusUnauthorized))

	// --- Test: Missing subject ---
	claims = defaultClaims()
	delete(claims, "sub")
	req7 := httptest.NewRequest("GET", "/", nil)
	req7.Header.Set("Authorization", "Bearer "+env.signToken(t, claims))
	w7 := httptest.NewRecorder()
	handler.ServeHTTP(w7, req7)
	Expect(w7.Code).To(Equal(http.StatusUnauthorized))
}

func TestAuthMiddleware_ExtractsDatasourceAndRemovesAuthorization(t *testing.T) {
	RegisterTestingT(t)
	env := setupJWKS(t)

	finalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Expect(r.Header.Get("Authorization")).To(BeEmpty())
		Expect(r.Context().Value(ContextDataSourceID)).To(Equal("prometheus-prod"))
		Expect(r.Context().Value(ContextDataSourceType)).To(Equal("prometheus"))
		io.WriteString(w, "OK")
	})

	handler := authMiddleware(finalHandler)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+env.signToken(t, defaultClaims()))
	req.Header.Set("X-Datasource-Uid", "prometheus-prod")
	req.Header.Set("X-Datasource-Type", "prometheus")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	Expect(w.Code).To(Equal(http.StatusOK))
}

func TestGlobToRegex(t *testing.T) {
	RegisterTestingT(t)
	testCases := []struct {
		pattern string
		want    string
	}{
		{"monitoring", "monitoring"},
		{"*", ".+"},
		{"dev-*", "dev-.*"},
		{"backend-?", "backend-."},
		{"team-a.b", `team-a\.b`},
	}
	for _, tc := range testCases {
		Expect(globToRegex(tc.pattern)).To(Equal(tc.want), "pattern %q", tc.pattern)
	}
}
