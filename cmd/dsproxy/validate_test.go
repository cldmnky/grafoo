package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	. "github.com/onsi/gomega"
)

func TestInitJWKSAndVerifyBearerToken(t *testing.T) {
	RegisterTestingT(t)
	env := setupJWKS(t)

	// Valid token
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+env.signToken(t, defaultClaims()))
	parsed, err := verifyBearerToken(req)
	Expect(err).To(BeNil())
	Expect(parsed).ToNot(BeNil())
	Expect(parsed.Valid).To(BeTrue())

	// Missing Authorization header
	req2 := httptest.NewRequest("GET", "/", nil)
	_, err = verifyBearerToken(req2)
	Expect(err).ToNot(BeNil())

	// Invalid token
	req3 := httptest.NewRequest("GET", "/", nil)
	req3.Header.Set("Authorization", "Bearer invalidtoken")
	_, err = verifyBearerToken(req3)
	Expect(err).ToNot(BeNil())

	// Wrong prefix
	req4 := httptest.NewRequest("GET", "/", nil)
	req4.Header.Set("Authorization", "Token "+env.signToken(t, defaultClaims()))
	_, err = verifyBearerToken(req4)
	Expect(err).ToNot(BeNil())
}

func TestTokenWithoutExpIsRejected(t *testing.T) {
	RegisterTestingT(t)
	env := setupJWKS(t)

	claims := defaultClaims()
	delete(claims, "exp")

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+env.signToken(t, claims))
	_, err := verifyBearerToken(req)
	Expect(err).ToNot(BeNil())
}

func TestTokenWithArrayAudienceIsAccepted(t *testing.T) {
	RegisterTestingT(t)
	env := setupJWKS(t)

	claims := defaultClaims()
	claims["aud"] = []string{"some-other-app", f_jwtAudience}

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+env.signToken(t, claims))
	_, err := verifyBearerToken(req)
	Expect(err).To(BeNil())
}

func TestTokenWithWrongAudienceIsRejected(t *testing.T) {
	RegisterTestingT(t)
	env := setupJWKS(t)

	claims := defaultClaims()
	claims["aud"] = "wrong-audience"

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+env.signToken(t, claims))
	_, err := verifyBearerToken(req)
	Expect(err).ToNot(BeNil())
}

func TestTokenWithExpiredTimestampIsRejected(t *testing.T) {
	RegisterTestingT(t)
	env := setupJWKS(t)

	claims := defaultClaims()
	claims["exp"] = time.Now().Add(-time.Hour).Unix()

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+env.signToken(t, claims))
	_, err := verifyBearerToken(req)
	Expect(err).ToNot(BeNil())
}

func TestTokenIssuerValidation(t *testing.T) {
	RegisterTestingT(t)
	env := setupJWKS(t)

	origIssuer := f_jwtIssuer
	f_jwtIssuer = "https://expected-issuer.example.com"
	t.Cleanup(func() { f_jwtIssuer = origIssuer })

	// Wrong issuer is rejected
	claims := defaultClaims()
	claims["iss"] = "https://other-issuer.example.com"
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+env.signToken(t, claims))
	_, err := verifyBearerToken(req)
	Expect(err).ToNot(BeNil())

	// Missing issuer is rejected when configured
	claims = defaultClaims()
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+env.signToken(t, claims))
	_, err = verifyBearerToken(req)
	Expect(err).ToNot(BeNil())

	// Correct issuer is accepted
	claims = defaultClaims()
	claims["iss"] = "https://expected-issuer.example.com"
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+env.signToken(t, claims))
	_, err = verifyBearerToken(req)
	Expect(err).To(BeNil())
}

func TestSymmetricSigningAlgorithmsAreRejected(t *testing.T) {
	RegisterTestingT(t)
	setupJWKS(t)

	// Sign an HS256 token with a bogus symmetric key. It must be rejected
	// regardless of signature validity because HS* is not allowed.
	token := newTestToken(defaultClaims(), "HS256")
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	_, err := verifyBearerToken(req)
	Expect(err).ToNot(BeNil())
}

func TestInitJWKSDiscoveryFailures(t *testing.T) {
	t.Run("non-200 discovery response", func(t *testing.T) {
		RegisterTestingT(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer srv.Close()

		orig := f_jwksURL
		origJWKS := jwks
		f_jwksURL = srv.URL
		jwks = nil
		defer func() { f_jwksURL = orig; jwks = origJWKS }()

		err := initJWKS()
		Expect(err).ToNot(BeNil())
	})

	t.Run("discovery response missing jwks_uri", func(t *testing.T) {
		RegisterTestingT(t)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"issuer":"https://example.com"}`))
		}))
		defer srv.Close()

		orig := f_jwksURL
		origJWKS := jwks
		f_jwksURL = srv.URL
		jwks = nil
		defer func() { f_jwksURL = orig; jwks = origJWKS }()

		err := initJWKS()
		Expect(err).ToNot(BeNil())
	})

	t.Run("unreachable discovery endpoint", func(t *testing.T) {
		RegisterTestingT(t)
		orig := f_jwksURL
		origJWKS := jwks
		f_jwksURL = "http://127.0.0.1:1/.well-known/openid-configuration"
		jwks = nil
		defer func() { f_jwksURL = orig; jwks = origJWKS }()

		err := initJWKS()
		Expect(err).ToNot(BeNil())
	})
}
