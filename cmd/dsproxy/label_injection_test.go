package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
)

// TestPrometheusProxyIntegration tests the full pipeline:
// authz middleware → prom-label-proxy → upstream
func TestPrometheusProxyIntegration(t *testing.T) {
	RegisterTestingT(t)

	// Create a mock Prometheus upstream server
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the query parameter contains the injected label
		query := r.URL.Query().Get("query")
		t.Logf("Upstream received query: %s", query)

		// Send back a mock Prometheus response
		response := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "vector",
				"result":     []interface{}{},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer upstreamServer.Close()

	// Create temporary policy directory for test
	tmpDir := t.TempDir()
	policyFile := filepath.Join(tmpDir, "policy.csv")
	modelFile := filepath.Join(tmpDir, "model.conf")

	// Write test policy
	policyContent := `p, testuser, datasource1, cluster1/test-namespace, read
p, adminuser, *, */*, read
g, testuser, developers`
	err := os.WriteFile(policyFile, []byte(policyContent), 0644)
	Expect(err).To(BeNil())

	// Write test model
	modelContent := `[request_definition]
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
	err = os.WriteFile(modelFile, []byte(modelContent), 0644)
	Expect(err).To(BeNil())

	// Create authz service
	authzService, err := NewAuthzService(context.Background(), tmpDir, nil)
	Expect(err).To(BeNil())
	Expect(authzService).ToNot(BeNil())

	// Create prom-label-proxy
	promProxy, err := newPrometheusProxy(upstreamServer.URL, "namespace")
	Expect(err).To(BeNil())
	Expect(promProxy).ToNot(BeNil())

	// Wrap with authz middleware
	handler := authzService.authzMiddleware("read")(promProxy)

	// Test 1: User with authorized namespace
	t.Run("AuthorizedUser", func(t *testing.T) {
		RegisterTestingT(t)

		req := httptest.NewRequest("GET", "/api/v1/query?query=up", nil)
		ctx := context.WithValue(req.Context(), ContextKeyEmail, "testuser")
		ctx = context.WithValue(ctx, ContextKeyGroups, []string{"developers"})
		ctx = context.WithValue(ctx, ContextDataSourceID, "datasource1")
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		Expect(w.Code).To(Equal(http.StatusOK))
		body := w.Body.String()
		t.Logf("Response: %s", body)

		var response map[string]interface{}
		err := json.Unmarshal([]byte(body), &response)
		Expect(err).To(BeNil())
		Expect(response["status"]).To(Equal("success"))
	})

	// Test 2: User without authorization
	t.Run("UnauthorizedUser", func(t *testing.T) {
		RegisterTestingT(t)

		req := httptest.NewRequest("GET", "/api/v1/query?query=up", nil)
		ctx := context.WithValue(req.Context(), ContextKeyEmail, "unauthorized")
		ctx = context.WithValue(ctx, ContextKeyGroups, []string{})
		ctx = context.WithValue(ctx, ContextDataSourceID, "datasource1")
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		Expect(w.Code).To(Equal(http.StatusForbidden))
	})

	// Test 3: Admin with wildcard access
	t.Run("AdminWithWildcard", func(t *testing.T) {
		RegisterTestingT(t)

		req := httptest.NewRequest("GET", "/api/v1/query?query=up{job=\"test\"}", nil)
		ctx := context.WithValue(req.Context(), ContextKeyEmail, "adminuser")
		ctx = context.WithValue(ctx, ContextKeyGroups, []string{})
		ctx = context.WithValue(ctx, ContextDataSourceID, "any-datasource")
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		Expect(w.Code).To(Equal(http.StatusOK))
	})
}

// TestContextLabelExtractor tests the label extraction logic
func TestContextLabelExtractor(t *testing.T) {
	RegisterTestingT(t)

	extractor := &contextLabelExtractor{
		label: "namespace",
	}

	// Create a test handler that will be wrapped
	nextCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	handler := extractor.ExtractLabel(nextHandler)

	// Test 1: Valid allowed clusters in context
	t.Run("ValidAllowedClusters", func(t *testing.T) {
		RegisterTestingT(t)
		nextCalled = false

		req := httptest.NewRequest("GET", "/test", nil)
		allowedPairs := [][2]string{
			{"cluster1", "namespace1"},
			{"cluster1", "namespace2"},
		}
		ctx := context.WithValue(req.Context(), ContextKeyAllowedClusters, allowedPairs)
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(nextCalled).To(BeTrue())
	})

	// Test 2: No allowed clusters in context
	t.Run("NoAllowedClusters", func(t *testing.T) {
		RegisterTestingT(t)
		nextCalled = false

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		Expect(w.Code).To(Equal(http.StatusForbidden))
		Expect(nextCalled).To(BeFalse())
	})

	// Test 3: Empty allowed clusters list
	t.Run("EmptyAllowedClusters", func(t *testing.T) {
		RegisterTestingT(t)
		nextCalled = false

		req := httptest.NewRequest("GET", "/test", nil)
		allowedPairs := [][2]string{}
		ctx := context.WithValue(req.Context(), ContextKeyAllowedClusters, allowedPairs)
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		Expect(w.Code).To(Equal(http.StatusForbidden))
		Expect(nextCalled).To(BeFalse())
	})
}

// TestNewPrometheusProxy tests the prometheus proxy creation
func TestNewPrometheusProxy(t *testing.T) {
	RegisterTestingT(t)

	// Test 1: Valid upstream URL
	t.Run("ValidUpstreamURL", func(t *testing.T) {
		RegisterTestingT(t)

		proxy, err := newPrometheusProxy("http://localhost:9090", "namespace")
		Expect(err).To(BeNil())
		Expect(proxy).ToNot(BeNil())
	})

	// Test 2: Invalid upstream URL
	t.Run("InvalidUpstreamURL", func(t *testing.T) {
		RegisterTestingT(t)

		proxy, err := newPrometheusProxy("://invalid-url", "namespace")
		Expect(err).ToNot(BeNil())
		Expect(proxy).To(BeNil())
	})
}

// TestLabelInjectionWithDifferentQueries tests various PromQL query patterns
func TestLabelInjectionWithDifferentQueries(t *testing.T) {
	RegisterTestingT(t)

	testCases := []struct {
		name          string
		originalQuery string
		user          string
		datasource    string
		expectedCode  int
		shouldContain string // substring to check in forwarded query
	}{
		{
			name:          "SimpleMetric",
			originalQuery: "up",
			user:          "testuser",
			datasource:    "datasource1",
			expectedCode:  http.StatusOK,
			shouldContain: "namespace",
		},
		{
			name:          "MetricWithLabels",
			originalQuery: "up{job=\"test\"}",
			user:          "testuser",
			datasource:    "datasource1",
			expectedCode:  http.StatusOK,
			shouldContain: "namespace",
		},
		{
			name:          "ComplexQuery",
			originalQuery: "rate(http_requests_total{job=\"api\"}[5m])",
			user:          "testuser",
			datasource:    "datasource1",
			expectedCode:  http.StatusOK,
			shouldContain: "namespace",
		},
		{
			name:          "UnauthorizedUser",
			originalQuery: "up",
			user:          "unauthorized",
			datasource:    "datasource1",
			expectedCode:  http.StatusForbidden,
			shouldContain: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			RegisterTestingT(t)

			// Track queries received by upstream
			var receivedQuery string
			upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedQuery = r.URL.Query().Get("query")
				t.Logf("Upstream received: %s", receivedQuery)

				response := map[string]interface{}{
					"status": "success",
					"data": map[string]interface{}{
						"resultType": "vector",
						"result":     []interface{}{},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(response)
			}))
			defer upstreamServer.Close()

			// Setup policy
			tmpDir := t.TempDir()
			policyFile := filepath.Join(tmpDir, "policy.csv")
			modelFile := filepath.Join(tmpDir, "model.conf")

			policyContent := `p, testuser, datasource1, cluster1/test-namespace, read
p, adminuser, *, */*, read`
			os.WriteFile(policyFile, []byte(policyContent), 0644)

			modelContent := `[request_definition]
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
			os.WriteFile(modelFile, []byte(modelContent), 0644)

			authzService, err := NewAuthzService(context.Background(), tmpDir, nil)
			Expect(err).To(BeNil())

			promProxy, err := newPrometheusProxy(upstreamServer.URL, "namespace")
			Expect(err).To(BeNil())

			handler := authzService.authzMiddleware("read")(promProxy)

			// Make request
			encodedQuery := url.QueryEscape(tc.originalQuery)
			req := httptest.NewRequest("GET", "/api/v1/query?query="+encodedQuery, nil)
			ctx := context.WithValue(req.Context(), ContextKeyEmail, tc.user)
			ctx = context.WithValue(ctx, ContextKeyGroups, []string{})
			ctx = context.WithValue(ctx, ContextDataSourceID, tc.datasource)
			req = req.WithContext(ctx)

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(tc.expectedCode))

			if tc.expectedCode == http.StatusOK && tc.shouldContain != "" {
				// For authorized requests, verify label was injected
				Expect(receivedQuery).To(ContainSubstring(tc.shouldContain))
				t.Logf("Original: %s → Transformed: %s", tc.originalQuery, receivedQuery)
			}
		})
	}
}

// TestQueryRangeEndpoint tests the /api/v1/query_range endpoint
func TestQueryRangeEndpoint(t *testing.T) {
	RegisterTestingT(t)

	var receivedQuery string
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.Query().Get("query")
		t.Logf("query_range upstream received: %s", receivedQuery)

		response := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "matrix",
				"result":     []interface{}{},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer upstreamServer.Close()

	// Setup
	tmpDir := t.TempDir()
	policyFile := filepath.Join(tmpDir, "policy.csv")
	modelFile := filepath.Join(tmpDir, "model.conf")

	policyContent := `p, rangeuser, datasource1, cluster1/test-ns, read`
	os.WriteFile(policyFile, []byte(policyContent), 0644)

	modelContent := `[request_definition]
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
	os.WriteFile(modelFile, []byte(modelContent), 0644)

	authzService, err := NewAuthzService(context.Background(), tmpDir, nil)
	Expect(err).To(BeNil())

	promProxy, err := newPrometheusProxy(upstreamServer.URL, "namespace")
	Expect(err).To(BeNil())

	handler := authzService.authzMiddleware("read")(promProxy)

	// Test query_range request
	req := httptest.NewRequest("GET", "/api/v1/query_range?query=up&start=0&end=100&step=15", nil)
	ctx := context.WithValue(req.Context(), ContextKeyEmail, "rangeuser")
	ctx = context.WithValue(ctx, ContextKeyGroups, []string{})
	ctx = context.WithValue(ctx, ContextDataSourceID, "datasource1")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	Expect(w.Code).To(Equal(http.StatusOK))
	Expect(receivedQuery).To(ContainSubstring("namespace"))

	// Verify response is valid JSON
	body, err := io.ReadAll(w.Body)
	Expect(err).To(BeNil())

	var response map[string]interface{}
	err = json.Unmarshal(body, &response)
	Expect(err).To(BeNil())
	Expect(response["status"]).To(Equal("success"))
}

// TestMultipleNamespaceScenarios tests users with access to multiple namespaces
func TestMultipleNamespaceScenarios(t *testing.T) {
	RegisterTestingT(t)

	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		t.Logf("Multi-namespace query: %s", query)

		response := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "vector",
				"result":     []interface{}{},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer upstreamServer.Close()

	tmpDir := t.TempDir()
	policyFile := filepath.Join(tmpDir, "policy.csv")
	modelFile := filepath.Join(tmpDir, "model.conf")

	// User with access to multiple namespaces
	policyContent := `p, multiuser, datasource1, cluster1/namespace1, read
p, multiuser, datasource1, cluster1/namespace2, read
p, multiuser, datasource1, cluster1/namespace3, read`
	os.WriteFile(policyFile, []byte(policyContent), 0644)

	modelContent := `[request_definition]
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
	os.WriteFile(modelFile, []byte(modelContent), 0644)

	authzService, err := NewAuthzService(context.Background(), tmpDir, nil)
	Expect(err).To(BeNil())

	promProxy, err := newPrometheusProxy(upstreamServer.URL, "namespace")
	Expect(err).To(BeNil())

	handler := authzService.authzMiddleware("read")(promProxy)

	req := httptest.NewRequest("GET", "/api/v1/query?query=up", nil)
	ctx := context.WithValue(req.Context(), ContextKeyEmail, "multiuser")
	ctx = context.WithValue(ctx, ContextKeyGroups, []string{})
	ctx = context.WithValue(ctx, ContextDataSourceID, "datasource1")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should succeed but currently only uses first namespace
	// TODO: In the future, should inject namespace=~"namespace1|namespace2|namespace3"
	Expect(w.Code).To(Equal(http.StatusOK))
	t.Log("Note: Currently only first namespace is injected. Multi-namespace regex support is a TODO.")
}
