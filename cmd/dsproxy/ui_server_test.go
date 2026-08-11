package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

func authenticatedRequest(env *jwksTestEnv, t *testing.T, method, path string, body string) *http.Request {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Authorization", "Bearer "+env.signToken(t, defaultClaims()))
	return req
}

func TestUIPolicyAPIRequiresAuthentication(t *testing.T) {
	RegisterTestingT(t)
	setupJWKS(t)
	authz := setupAuthz(t, "p, alice, *, cluster1/ns1, read\n")
	handler := newUIHandler(authz)

	// No token
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/policy", nil))
	Expect(w.Code).To(Equal(http.StatusUnauthorized))

	// Invalid token
	w = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/policy", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	handler.ServeHTTP(w, req)
	Expect(w.Code).To(Equal(http.StatusUnauthorized))
}

func TestUIPolicyAPIGetAndUpdate(t *testing.T) {
	RegisterTestingT(t)
	env := setupJWKS(t)
	authz := setupAuthz(t, "p, alice, *, cluster1/ns1, read\n")
	handler := newUIHandler(authz)

	// GET returns the current policy
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, authenticatedRequest(env, t, "GET", "/api/policy", ""))
	Expect(w.Code).To(Equal(http.StatusOK))
	var data PolicyData
	Expect(json.NewDecoder(w.Body).Decode(&data)).To(Succeed())
	Expect(data.Policies).To(Equal([][]string{{"alice", "*", "cluster1/ns1", "read"}}))

	// POST replaces the policy
	body := `{"policies":[["bob","*","cluster2/ns2","read"],["team","prometheus-prod","cluster1/ns-*","read"]],"groupingPolicies":[["bob","team"]]}`
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, authenticatedRequest(env, t, "POST", "/api/policy", body))
	Expect(w.Code).To(Equal(http.StatusOK))

	// The on-disk policy file is updated
	content, err := os.ReadFile(authz.policyFile)
	Expect(err).To(BeNil())
	Expect(string(content)).To(ContainSubstring("bob, *, cluster2/ns2, read"))
	Expect(string(content)).To(ContainSubstring("g, bob, team"))

	// The enforcer is updated
	policies, err := authz.Enforcer.GetPolicy()
	Expect(err).To(BeNil())
	Expect(policies).To(HaveLen(2))
	grouping, err := authz.Enforcer.GetGroupingPolicy()
	Expect(err).To(BeNil())
	Expect(grouping).To(Equal([][]string{{"bob", "team"}}))

	// GET now returns the new policy
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, authenticatedRequest(env, t, "GET", "/api/policy", ""))
	Expect(w.Code).To(Equal(http.StatusOK))
	var updated PolicyData
	Expect(json.NewDecoder(w.Body).Decode(&updated)).To(Succeed())
	Expect(updated.Policies[0]).To(Equal([]string{"bob", "*", "cluster2/ns2", "read"}))
	Expect(updated.GroupingPolicies).To(Equal([][]string{{"bob", "team"}}))
}

func TestUIPolicyAPIRejectsInvalidPayloads(t *testing.T) {
	RegisterTestingT(t)
	env := setupJWKS(t)
	authz := setupAuthz(t, "p, alice, *, cluster1/ns1, read\n")
	handler := newUIHandler(authz)

	invalidBodies := []string{
		`{"policies":[["alice","*","cluster1/ns1"]]}`,                // wrong arity
		`{"policies":[["alice","*","cluster1/ns1","read","extra"]]}`, // too many fields
		`{"policies":[["","*","cluster1/ns1","read"]]}`,              // empty field
		`{"groupingPolicies":[["alice"]]}`,                           // wrong arity
		`{"policies":["not-a-list"]}`,                                // malformed
		`not-json`,                                                   // invalid json
	}
	for _, body := range invalidBodies {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, authenticatedRequest(env, t, "POST", "/api/policy", body))
		Expect(w.Code).To(Equal(http.StatusBadRequest), "body: %s", body)
	}

	// The original policy must be untouched after rejected updates
	policies, err := authz.Enforcer.GetPolicy()
	Expect(err).To(BeNil())
	Expect(policies).To(Equal([][]string{{"alice", "*", "cluster1/ns1", "read"}}))
}

func TestUIPolicyAPIFailedSaveDoesNotLosePolicy(t *testing.T) {
	RegisterTestingT(t)
	env := setupJWKS(t)
	authz := setupAuthz(t, "p, alice, *, cluster1/ns1, read\n")

	// Make the policy directory read-only so the save fails (skip as root)
	dir := filepath.Dir(authz.policyFile)
	if os.Geteuid() == 0 {
		t.Skip("cannot test read-only policy directory as root")
	}
	Expect(os.Chmod(dir, 0555)).To(Succeed())
	t.Cleanup(func() { os.Chmod(dir, 0755) })

	handler := newUIHandler(authz)
	body := `{"policies":[["bob","*","cluster2/ns2","read"]]}`
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, authenticatedRequest(env, t, "POST", "/api/policy", body))
	Expect(w.Code).To(Equal(http.StatusInternalServerError))

	// The in-memory policy is unchanged after the failed save
	policies, err := authz.Enforcer.GetPolicy()
	Expect(err).To(BeNil())
	Expect(policies).To(Equal([][]string{{"alice", "*", "cluster1/ns1", "read"}}))
}

func TestUIServesStaticAssets(t *testing.T) {
	RegisterTestingT(t)
	setupJWKS(t)
	authz := setupAuthz(t, "p, alice, *, cluster1/ns1, read\n")
	handler := newUIHandler(authz)

	// index.html is served for "/"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	Expect(w.Code).To(Equal(http.StatusOK))
	Expect(w.Body.String()).To(ContainSubstring("<!doctype html>"))

	// Unknown SPA routes fall back to index.html
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/policy", nil))
	Expect(w.Code).To(Equal(http.StatusOK))

	// Unknown API routes are not served
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/unknown", nil))
	Expect(w.Code).To(Equal(http.StatusNotFound))
}
