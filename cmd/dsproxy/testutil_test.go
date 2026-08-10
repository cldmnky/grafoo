package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// jwksTestEnv provides the RSA key used to sign tokens in tests.
type jwksTestEnv struct {
	key *rsa.PrivateKey
}

// setupJWKS starts mock OIDC discovery + JWKS servers backed by an RSA key
// and initializes the global jwks and the JWT audience used by the parser.
// The cleanup restores the previous global state.
func setupJWKS(t *testing.T) *jwksTestEnv {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	n := base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes())
	jwksJSON := fmt.Sprintf(`{"keys":[{"kty":"RSA","kid":"test","n":"%s","e":"%s"}]}`, n, e)

	jwksSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, jwksJSON)
	}))
	discoverySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"jwks_uri": jwksSrv.URL})
	}))

	origJWKSURL := f_jwksURL
	origAudience := f_jwtAudience
	origJWKS := jwks
	f_jwksURL = discoverySrv.URL
	f_jwtAudience = "test-audience"
	t.Cleanup(func() {
		discoverySrv.Close()
		jwksSrv.Close()
		f_jwksURL = origJWKSURL
		f_jwtAudience = origAudience
		jwks = origJWKS
	})

	if err := initJWKS(); err != nil {
		t.Fatalf("initJWKS failed: %v", err)
	}
	if jwks == nil {
		t.Fatal("jwks not initialized")
	}

	return &jwksTestEnv{key: key}
}

// signToken creates and signs a JWT with the given claims using RS256 and the
// test key's kid.
func (env *jwksTestEnv) signToken(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "test"
	signed, err := token.SignedString(env.key)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return signed
}

// defaultClaims returns a set of claims that pass all JWT validation.
func defaultClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"sub":    "1234567890",
		"aud":    f_jwtAudience,
		"exp":    time.Now().Add(time.Hour).Unix(),
		"iat":    time.Now().Unix(),
		"groups": []string{"system:authenticated", "system:cluster-admins"},
	}
}

// newTestToken signs a token with the given method using a random symmetric
// key, simulating a token signed with a disallowed algorithm.
func newTestToken(claims jwt.MapClaims, method string) string {
	token := jwt.NewWithClaims(jwt.GetSigningMethod(method), claims)
	token.Header["kid"] = "test"
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	signed, err := token.SignedString(key)
	if err != nil {
		return "unable-to-sign:" + err.Error()
	}
	return signed
}

// testModel is the Casbin model used by integration tests. It matches the
// shipped model.conf (RBAC with keyMatch2 wildcards).
const testModel = `[request_definition]
r = sub, dom, obj, act

[policy_definition]
p = sub, dom, obj, act

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = (g(r.sub, p.sub) || r.sub == p.sub) &&
    (keyMatch2(r.dom, p.dom) || p.dom == "*") &&
    (keyMatch2(r.obj, p.obj) || p.obj == "*") &&
    r.act == p.act`

// setupAuthz creates an AuthzService from the given policy CSV in a temp
// directory. The file watcher goroutine is stopped via t.Cleanup.
func setupAuthz(t *testing.T, policyCSV string) *AuthzService {
	t.Helper()
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "model.conf"), []byte(testModel), 0644); err != nil {
		t.Fatalf("failed to write model: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "policy.csv"), []byte(policyCSV), 0644); err != nil {
		t.Fatalf("failed to write policy: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	authz, err := NewAuthzService(ctx, tmpDir)
	if err != nil {
		t.Fatalf("failed to create authz service: %v", err)
	}
	return authz
}
