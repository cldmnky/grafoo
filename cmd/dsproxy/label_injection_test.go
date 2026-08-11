package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"github.com/prometheus-community/prom-label-proxy/injectproxy"
)

// newUpstream captures the queries (query or match[] params) it receives and
// returns a canned Prometheus response.
func newUpstream(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var received []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query().Get("query"); q != "" {
			received = append(received, q)
		}
		for _, m := range r.URL.Query()["match[]"] {
			received = append(received, m)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "vector",
				"result":     []interface{}{},
			},
		})
	}))
	t.Cleanup(server.Close)
	return server, &received
}

// newProxyChain builds the authz middleware wrapping a prom-label-proxy
// handler pointed at the given upstream, using the default label names
// (cluster + namespace).
func newProxyChain(t *testing.T, policyCSV string, upstreamURL string) http.Handler {
	return newProxyChainWithLabels(t, policyCSV, upstreamURL, "namespace", "cluster")
}

// newProxyChainWithLabels builds the authz middleware wrapping a
// prom-label-proxy handler with explicit label names.
func newProxyChainWithLabels(t *testing.T, policyCSV string, upstreamURL, namespaceLabel, clusterLabel string) http.Handler {
	t.Helper()
	authz := setupAuthz(t, policyCSV)
	promProxy, cleanup, err := newPrometheusProxy(upstreamURL, namespaceLabel, clusterLabel)
	if err != nil {
		t.Fatalf("failed to create prometheus proxy: %v", err)
	}
	t.Cleanup(cleanup)
	return authz.authzMiddleware("read")(promProxy)
}

func requestWithContext(t *testing.T, handler http.Handler, method, path string, user string, datasource string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	ctx := context.WithValue(req.Context(), ContextKeyEmail, user)
	ctx = context.WithValue(ctx, ContextKeyGroups, []string{})
	ctx = context.WithValue(ctx, ContextDataSourceID, datasource)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

// TestPrometheusProxyIntegration tests the full pipeline:
// authz middleware → prom-label-proxy → upstream
func TestPrometheusProxyIntegration(t *testing.T) {
	RegisterTestingT(t)
	upstream, received := newUpstream(t)

	policy := `p, testuser, datasource1, cluster1/test-namespace, read
p, adminuser, *, */*, read
g, testuser, developers`
	handler := newProxyChain(t, policy, upstream.URL)

	// Test 1: User with authorized namespace
	w := requestWithContext(t, handler, "GET", "/api/v1/query?query=up", "testuser", "datasource1")
	Expect(w.Code).To(Equal(http.StatusOK))
	Expect((*received)[len(*received)-1]).To(Equal(`up{cluster=~"cluster1",namespace=~"test-namespace"}`))

	// Test 2: User without authorization
	w = requestWithContext(t, handler, "GET", "/api/v1/query?query=up", "unauthorized", "datasource1")
	Expect(w.Code).To(Equal(http.StatusForbidden))

	// Test 3: Admin with wildcard access. Prometheus sorts label matchers
	// alphabetically, so the injected labels appear before "job".
	w = requestWithContext(t, handler, "GET", `/api/v1/query?query=up{job="test"}`, "adminuser", "any-datasource")
	Expect(w.Code).To(Equal(http.StatusOK))
	Expect((*received)[len(*received)-1]).To(Equal(`up{cluster=~".+",job="test",namespace=~".+"}`))
}

// TestWildcardPolicyInjection verifies that keyMatch2-style patterns are
// translated into PromQL regex matchers.
func TestWildcardPolicyInjection(t *testing.T) {
	RegisterTestingT(t)

	t.Run("namespace prefix wildcard", func(t *testing.T) {
		RegisterTestingT(t)
		upstream, received := newUpstream(t)
		handler := newProxyChain(t, "p, bob, *, cluster1/dev-*, read", upstream.URL)
		w := requestWithContext(t, handler, "GET", "/api/v1/query?query=up", "bob", "datasource1")
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect((*received)[len(*received)-1]).To(Equal(`up{cluster=~"cluster1",namespace=~"dev-.*"}`))
	})

	t.Run("any cluster pattern", func(t *testing.T) {
		RegisterTestingT(t)
		upstream, received := newUpstream(t)
		handler := newProxyChain(t, "p, qa, *, */qa-*, read", upstream.URL)
		w := requestWithContext(t, handler, "GET", "/api/v1/query?query=up", "qa", "datasource1")
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect((*received)[len(*received)-1]).To(Equal(`up{cluster=~".+",namespace=~"qa-.*"}`))
	})

	t.Run("all namespaces in a cluster", func(t *testing.T) {
		RegisterTestingT(t)
		upstream, received := newUpstream(t)
		handler := newProxyChain(t, "p, sre, *, prod-cluster/*, read", upstream.URL)
		w := requestWithContext(t, handler, "GET", "/api/v1/query?query=up", "sre", "datasource1")
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect((*received)[len(*received)-1]).To(Equal(`up{cluster=~"prod-cluster",namespace=~".+"}`))
	})
}

// TestMultiNamespaceInjection verifies that a user authorized for several
// namespaces gets a deterministic regex union injected.
func TestMultiNamespaceInjection(t *testing.T) {
	RegisterTestingT(t)
	upstream, received := newUpstream(t)
	policy := `p, multiuser, datasource1, cluster1/namespace3, read
p, multiuser, datasource1, cluster1/namespace1, read
p, multiuser, datasource1, cluster1/namespace2, read`
	handler := newProxyChain(t, policy, upstream.URL)

	// Multiple requests must produce the same sorted injection
	for i := 0; i < 5; i++ {
		w := requestWithContext(t, handler, "GET", "/api/v1/query?query=up", "multiuser", "datasource1")
		Expect(w.Code).To(Equal(http.StatusOK))
		Expect((*received)[len(*received)-1]).To(Equal(`up{cluster=~"cluster1",namespace=~"namespace1|namespace2|namespace3"}`))
	}
}

// TestSameNamespaceDifferentClusters verifies that the same namespace name
// authorized in multiple clusters is injected once while the cluster matcher
// covers both clusters.
func TestSameNamespaceDifferentClusters(t *testing.T) {
	RegisterTestingT(t)
	upstream, received := newUpstream(t)
	policy := `p, user, datasource1, cluster1/shared-ns, read
p, user, datasource1, cluster2/shared-ns, read`
	handler := newProxyChain(t, policy, upstream.URL)
	w := requestWithContext(t, handler, "GET", "/api/v1/query?query=up", "user", "datasource1")
	Expect(w.Code).To(Equal(http.StatusOK))
	Expect((*received)[len(*received)-1]).To(Equal(`up{cluster=~"cluster1|cluster2",namespace=~"shared-ns"}`))
}

// TestCustomLabelNames verifies that the injected label names are
// configurable (e.g. k8s_cluster / k8s_namespace instead of cluster /
// namespace).
func TestCustomLabelNames(t *testing.T) {
	RegisterTestingT(t)
	upstream, received := newUpstream(t)
	handler := newProxyChainWithLabels(t, "p, alice, datasource1, cluster1/monitoring, read", upstream.URL, "k8s_namespace", "k8s_cluster")

	w := requestWithContext(t, handler, "GET", "/api/v1/query?query=up", "alice", "datasource1")
	Expect(w.Code).To(Equal(http.StatusOK))
	Expect((*received)[len(*received)-1]).To(Equal(`up{k8s_cluster=~"cluster1",k8s_namespace=~"monitoring"}`))
}

// TestSingleLabelMode verifies that setting an empty cluster label disables
// cluster injection (backwards compatible namespace-only behavior).
func TestSingleLabelMode(t *testing.T) {
	RegisterTestingT(t)
	upstream, received := newUpstream(t)
	handler := newProxyChainWithLabels(t, "p, alice, datasource1, cluster1/monitoring, read", upstream.URL, "namespace", "")

	w := requestWithContext(t, handler, "GET", "/api/v1/query?query=up", "alice", "datasource1")
	Expect(w.Code).To(Equal(http.StatusOK))
	Expect((*received)[len(*received)-1]).To(Equal(`up{namespace=~"monitoring"}`))
}

// TestUserNamespaceMatcherIsOverridden verifies that a user cannot escape
// tenant isolation by specifying their own namespace matcher. In regex match
// mode the user's matcher is preserved and ANDed with the injected regex, so
// a conflicting matcher simply yields no results (no data leakage).
func TestUserNamespaceMatcherIsOverridden(t *testing.T) {
	RegisterTestingT(t)
	upstream, received := newUpstream(t)
	handler := newProxyChain(t, "p, alice, datasource1, cluster1/monitoring, read", upstream.URL)

	// Query attempting to access another namespace. The proxy must not 403
	// (the user is authorized for monitoring) but the injected matcher is
	// ANDed with the user's matcher, so the query cannot match "database".
	w := requestWithContext(t, handler, "GET", `/api/v1/query?query=up{namespace="database"}`, "alice", "datasource1")
	Expect(w.Code).To(Equal(http.StatusOK))
	forwarded := (*received)[len(*received)-1]
	Expect(forwarded).To(ContainSubstring(`cluster=~"cluster1"`))
	Expect(forwarded).To(ContainSubstring(`namespace=~"monitoring"`))
	// The conflicting user matcher is preserved: namespace="database" AND
	// namespace=~"monitoring" can never match a series.
	Expect(forwarded).To(ContainSubstring(`namespace="database"`))
}

// TestSeriesMatcherInjection verifies that /api/v1/series gets the label
// injected into every match[] parameter.
func TestSeriesMatcherInjection(t *testing.T) {
	RegisterTestingT(t)
	upstream, received := newUpstream(t)
	handler := newProxyChain(t, "p, alice, datasource1, cluster1/monitoring, read", upstream.URL)

	w := requestWithContext(t, handler, "GET", `/api/v1/series?match[]={job="api"}`, "alice", "datasource1")
	Expect(w.Code).To(Equal(http.StatusOK))
	Expect((*received)[len(*received)-1]).To(Equal(`{job="api",cluster=~"cluster1",namespace=~"monitoring"}`))
}

// TestLabelsAPIEnabled verifies that the label metadata endpoints are
// proxied with label injection enabled.
func TestLabelsAPIEnabled(t *testing.T) {
	RegisterTestingT(t)
	upstream, received := newUpstream(t)
	handler := newProxyChain(t, "p, alice, datasource1, cluster1/monitoring, read", upstream.URL)

	w := requestWithContext(t, handler, "GET", `/api/v1/labels?match[]={job="api"}`, "alice", "datasource1")
	Expect(w.Code).To(Equal(http.StatusOK))
	Expect((*received)[len(*received)-1]).To(Equal(`{job="api",cluster=~"cluster1",namespace=~"monitoring"}`))

	w = requestWithContext(t, handler, "GET", `/api/v1/label/job/values?match[]={job="api"}`, "alice", "datasource1")
	Expect(w.Code).To(Equal(http.StatusOK))
	Expect((*received)[len(*received)-1]).To(Equal(`{job="api",cluster=~"cluster1",namespace=~"monitoring"}`))
}

// TestContextLabelExtractor tests the label extraction logic for both the
// namespace and the cluster part.
func TestContextLabelExtractor(t *testing.T) {
	RegisterTestingT(t)

	namespaceExtractor := &contextLabelExtractor{
		label: "namespace",
		part:  partNamespace,
	}
	clusterExtractor := &contextLabelExtractor{
		label: "cluster",
		part:  partCluster,
	}

	allowedPairs := [][2]string{
		{"cluster1", "namespace2"},
		{"cluster1", "dev-*"},
		{"cluster2", "namespace1"},
	}

	newTestHandler := func() (http.HandlerFunc, *[]string) {
		var extracted []string
		h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			extracted = injectproxy.MustLabelValues(r.Context())
			w.WriteHeader(http.StatusOK)
		})
		return h, &extracted
	}

	t.Run("NamespacePartFromContext", func(t *testing.T) {
		RegisterTestingT(t)
		next, extracted := newTestHandler()
		handler := namespaceExtractor.ExtractLabel(next)

		req := httptest.NewRequest("GET", "/test", nil)
		ctx := context.WithValue(req.Context(), ContextKeyAllowedClusters, allowedPairs)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req.WithContext(ctx))

		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(*extracted).To(Equal([]string{"dev-.*|namespace1|namespace2"}))
	})

	t.Run("ClusterPartFromContext", func(t *testing.T) {
		RegisterTestingT(t)
		next, extracted := newTestHandler()
		handler := clusterExtractor.ExtractLabel(next)

		req := httptest.NewRequest("GET", "/test", nil)
		ctx := context.WithValue(req.Context(), ContextKeyAllowedClusters, allowedPairs)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req.WithContext(ctx))

		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(*extracted).To(Equal([]string{"cluster1|cluster2"}))
	})

	t.Run("ClusterPartSetsInternalHeader", func(t *testing.T) {
		RegisterTestingT(t)
		// The outer (cluster) extractor must propagate the pairs to the inner
		// proxy via the internal header.
		var headerValue string
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			headerValue = r.Header.Get(internalAuthorizedResourcesHeader)
			w.WriteHeader(http.StatusOK)
		})
		handler := clusterExtractor.ExtractLabel(next)

		req := httptest.NewRequest("GET", "/test", nil)
		ctx := context.WithValue(req.Context(), ContextKeyAllowedClusters, allowedPairs)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req.WithContext(ctx))

		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(headerValue).ToNot(BeEmpty())
		pairs, err := decodeAllowedPairs(headerValue)
		Expect(err).To(BeNil())
		Expect(pairs).To(Equal(allowedPairs))
	})

	t.Run("NamespacePartFromInternalHeader", func(t *testing.T) {
		RegisterTestingT(t)
		// The inner (namespace) extractor reads the pairs from the header set
		// by the cluster extractor and removes the header so it does not
		// reach the upstream.
		next, extracted := newTestHandler()
		handler := namespaceExtractor.ExtractLabel(next)

		req := httptest.NewRequest("GET", "/test", nil)
		encoded, err := encodeAllowedPairs(allowedPairs)
		Expect(err).To(BeNil())
		req.Header.Set(internalAuthorizedResourcesHeader, encoded)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(*extracted).To(Equal([]string{"dev-.*|namespace1|namespace2"}))
		Expect(req.Header.Get(internalAuthorizedResourcesHeader)).To(BeEmpty())
	})

	t.Run("NoAllowedClusters", func(t *testing.T) {
		RegisterTestingT(t)
		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
			w.WriteHeader(http.StatusOK)
		})
		handler := namespaceExtractor.ExtractLabel(next)

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		Expect(w.Code).To(Equal(http.StatusForbidden))
		Expect(nextCalled).To(BeFalse())
	})

	t.Run("EmptyAllowedClusters", func(t *testing.T) {
		RegisterTestingT(t)
		nextCalled := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nextCalled = true
			w.WriteHeader(http.StatusOK)
		})
		handler := namespaceExtractor.ExtractLabel(next)

		req := httptest.NewRequest("GET", "/test", nil)
		ctx := context.WithValue(req.Context(), ContextKeyAllowedClusters, [][2]string{})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req.WithContext(ctx))

		Expect(w.Code).To(Equal(http.StatusForbidden))
		Expect(nextCalled).To(BeFalse())
	})
}

// TestNewPrometheusProxy tests the prometheus proxy creation
func TestNewPrometheusProxy(t *testing.T) {
	RegisterTestingT(t)

	// Test 1: Valid upstream URL in dual-label mode
	t.Run("ValidUpstreamURL", func(t *testing.T) {
		RegisterTestingT(t)

		proxy, cleanup, err := newPrometheusProxy("http://localhost:9090", "namespace", "cluster")
		t.Cleanup(cleanup)
		Expect(err).To(BeNil())
		Expect(proxy).ToNot(BeNil())
	})

	// Test 2: Valid upstream URL in single-label mode
	t.Run("ValidUpstreamURLSingleLabel", func(t *testing.T) {
		RegisterTestingT(t)

		proxy, cleanup, err := newPrometheusProxy("http://localhost:9090", "namespace", "")
		t.Cleanup(cleanup)
		Expect(err).To(BeNil())
		Expect(proxy).ToNot(BeNil())
	})

	// Test 3: Invalid upstream URL
	t.Run("InvalidUpstreamURL", func(t *testing.T) {
		RegisterTestingT(t)

		proxy, _, err := newPrometheusProxy("://invalid-url", "namespace", "cluster")
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
		expectedQuery string
	}{
		{
			name:          "SimpleMetric",
			originalQuery: "up",
			user:          "testuser",
			datasource:    "datasource1",
			expectedCode:  http.StatusOK,
			expectedQuery: `up{cluster=~"cluster1",namespace=~"test-namespace"}`,
		},
		{
			name:          "MetricWithLabels",
			originalQuery: `up{job="test"}`,
			user:          "testuser",
			datasource:    "datasource1",
			expectedCode:  http.StatusOK,
			expectedQuery: `up{cluster=~"cluster1",job="test",namespace=~"test-namespace"}`,
		},
		{
			name:          "ComplexQuery",
			originalQuery: `rate(http_requests_total{job="api"}[5m])`,
			user:          "testuser",
			datasource:    "datasource1",
			expectedCode:  http.StatusOK,
			expectedQuery: `rate(http_requests_total{cluster=~"cluster1",job="api",namespace=~"test-namespace"}[5m])`,
		},
		{
			name:          "UnauthorizedUser",
			originalQuery: "up",
			user:          "unauthorized",
			datasource:    "datasource1",
			expectedCode:  http.StatusForbidden,
			expectedQuery: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			RegisterTestingT(t)

			upstream, received := newUpstream(t)
			handler := newProxyChain(t, "p, testuser, datasource1, cluster1/test-namespace, read", upstream.URL)

			encodedQuery := url.QueryEscape(tc.originalQuery)
			w := requestWithContext(t, handler, "GET", "/api/v1/query?query="+encodedQuery, tc.user, tc.datasource)
			Expect(w.Code).To(Equal(tc.expectedCode))

			if tc.expectedCode == http.StatusOK {
				Expect((*received)[len(*received)-1]).To(Equal(tc.expectedQuery))
			}
		})
	}
}

// TestQueryRangeEndpoint tests the /api/v1/query_range endpoint
func TestQueryRangeEndpoint(t *testing.T) {
	RegisterTestingT(t)
	upstream, received := newUpstream(t)
	handler := newProxyChain(t, "p, rangeuser, datasource1, cluster1/test-ns, read", upstream.URL)

	w := requestWithContext(t, handler, "GET", "/api/v1/query_range?query=up&start=0&end=100&step=15", "rangeuser", "datasource1")
	Expect(w.Code).To(Equal(http.StatusOK))
	Expect((*received)[len(*received)-1]).To(Equal(`up{cluster=~"cluster1",namespace=~"test-ns"}`))
}

// TestMultipleNamespaceScenarios tests users with access to multiple namespaces
func TestMultipleNamespaceScenarios(t *testing.T) {
	RegisterTestingT(t)
	upstream, received := newUpstream(t)
	policy := `p, multiuser, datasource1, cluster1/namespace1, read
p, multiuser, datasource1, cluster1/namespace2, read
p, multiuser, datasource1, cluster1/namespace3, read`
	handler := newProxyChain(t, policy, upstream.URL)

	w := requestWithContext(t, handler, "GET", "/api/v1/query?query=up", "multiuser", "datasource1")
	Expect(w.Code).To(Equal(http.StatusOK))
	Expect((*received)[len(*received)-1]).To(Equal(`up{cluster=~"cluster1",namespace=~"namespace1|namespace2|namespace3"}`))
}

// TestUpstreamTLSWithCABundle verifies that the CA bundle is used to verify
// the upstream Prometheus server certificate.
func TestUpstreamTLSWithCABundle(t *testing.T) {
	RegisterTestingT(t)

	// Generate a self-signed certificate for the upstream server
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).To(BeNil())
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
		BasicConstraintsValid: true,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	Expect(err).To(BeNil())
	cert := tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  priv,
	}

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "data": map[string]interface{}{}})
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	server.StartTLS()
	defer server.Close()

	// Write the CA certificate to a temp file
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	Expect(os.WriteFile(caPath, certPEM, 0644)).To(Succeed())

	origCABundle := f_caBundle
	origDefaultTransport := http.DefaultTransport
	t.Cleanup(func() {
		f_caBundle = origCABundle
		http.DefaultTransport = origDefaultTransport
	})
	f_caBundle = caPath
	Expect(configureUpstreamTLS()).To(Succeed())

	handler := newProxyChain(t, "p, tlsuser, datasource1, cluster1/test-ns, read", server.URL)
	w := requestWithContext(t, handler, "GET", "/api/v1/query?query=up", "tlsuser", "datasource1")
	Expect(w.Code).To(Equal(http.StatusOK))
	Expect(w.Body.String()).To(ContainSubstring("success"))
}

// TestUpstreamTLSWithoutCABundleFails verifies that a self-signed upstream
// fails when the CA bundle is not trusted.
func TestUpstreamTLSWithoutCABundleFails(t *testing.T) {
	RegisterTestingT(t)

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).To(BeNil())
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
		BasicConstraintsValid: true,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	Expect(err).To(BeNil())
	cert := tls.Certificate{Certificate: [][]byte{derBytes}, PrivateKey: priv}

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	server.StartTLS()
	defer server.Close()

	// Ensure no CA bundle is configured. The default transport does not
	// trust the self-signed test certificate.
	origCABundle := f_caBundle
	t.Cleanup(func() { f_caBundle = origCABundle })
	f_caBundle = ""
	Expect(configureUpstreamTLS()).To(Succeed())

	handler := newProxyChain(t, "p, tlsuser, datasource1, cluster1/test-ns, read", server.URL)
	w := requestWithContext(t, handler, "GET", "/api/v1/query?query=up", "tlsuser", "datasource1")
	// Reverse proxy error handler returns 502 when the upstream TLS handshake fails
	Expect(w.Code).To(Equal(http.StatusBadGateway))
}

// TestPolicyHotReload verifies that changes to policy.csv are picked up
// without a restart.
func TestPolicyHotReload(t *testing.T) {
	RegisterTestingT(t)

	upstream, received := newUpstream(t)
	policyDir := t.TempDir()
	Expect(os.WriteFile(filepath.Join(policyDir, "model.conf"), []byte(testModel), 0644)).To(Succeed())
	policyPath := filepath.Join(policyDir, "policy.csv")
	Expect(os.WriteFile(policyPath, []byte("p, firstuser, datasource1, cluster1/ns1, read\n"), 0644)).To(Succeed())

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	authz, err := NewAuthzService(ctx, policyDir)
	Expect(err).To(BeNil())

	promProxy, cleanup, err := newPrometheusProxy(upstream.URL, "namespace", "cluster")
	t.Cleanup(cleanup)
	Expect(err).To(BeNil())
	handler := authz.authzMiddleware("read")(promProxy)

	// Initially authorized
	w := requestWithContext(t, handler, "GET", "/api/v1/query?query=up", "firstuser", "datasource1")
	Expect(w.Code).To(Equal(http.StatusOK))

	// Second user is not yet authorized
	w = requestWithContext(t, handler, "GET", "/api/v1/query?query=up", "seconduser", "datasource1")
	Expect(w.Code).To(Equal(http.StatusForbidden))

	// Reload the policy to add the second user
	newPolicy := "p, firstuser, datasource1, cluster1/ns1, read\np, seconduser, datasource1, cluster1/ns2, read\n"
	Expect(os.WriteFile(policyPath, []byte(newPolicy), 0644)).To(Succeed())

	// The watcher reloads asynchronously; wait for the new policy to apply.
	Eventually(func() int {
		w := requestWithContext(t, handler, "GET", "/api/v1/query?query=up", "seconduser", "datasource1")
		return w.Code
	}, 5*time.Second, 100*time.Millisecond).Should(Equal(http.StatusOK))
	Expect((*received)[len(*received)-1]).To(Equal(`up{cluster=~"cluster1",namespace=~"ns2"}`))
}
