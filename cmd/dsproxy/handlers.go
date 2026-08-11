package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/prometheus-community/prom-label-proxy/injectproxy"
)

type contextKey string

const (
	ContextKeyEmail           contextKey = "email"
	ContextKeyGroups          contextKey = "groups"
	ContextKeyNamespace       contextKey = "namespace"
	ContextKeyCluster         contextKey = "cluster"
	ContextDataSourceID       contextKey = "datasourceID"
	ContextDataSourceType     contextKey = "datasourceType"
	ContextKeyAllowedClusters contextKey = "allowed_clusters"
)

// internalAuthorizedResourcesHeader carries the authorized cluster/namespace
// pairs from the outer (cluster) proxy to the inner (namespace) proxy. Both
// proxies run in the same process but exchange data over an internal HTTP
// hop, where the request context is lost. The inner proxy removes the header
// before forwarding upstream.
const internalAuthorizedResourcesHeader = "X-Dsproxy-Authorized-Resources"

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := verifyBearerToken(r)
		if err != nil {
			log.Printf("Unauthorized: %v", err)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			http.Error(w, "Invalid token claims", http.StatusUnauthorized)
			return
		}

		// Extract sub (user email/identity) from JWT - this is what we'll use for authz
		sub, _ := claims["sub"].(string)
		if sub == "" {
			http.Error(w, "Missing subject claim", http.StatusUnauthorized)
			return
		}

		email, _ := claims["email"].(string)
		groups := extractStringSlice(claims["groups"])

		log.Printf("Authenticated user: %s (sub: %s), groups: %v", email, sub, groups)

		ctx := context.WithValue(r.Context(), ContextKeyEmail, sub) // Use sub as primary identity
		ctx = context.WithValue(ctx, ContextKeyGroups, groups)

		// Get ds from the header X-Datasource-Uid
		datasourceID := r.Header.Get("X-Datasource-Uid")
		if datasourceID != "" {
			ctx = context.WithValue(ctx, ContextDataSourceID, datasourceID)
		}

		// Detect datasource type from X-Datasource-Type header
		datasourceType := r.Header.Get("X-Datasource-Type")
		if datasourceType != "" {
			ctx = context.WithValue(ctx, ContextDataSourceType, datasourceType)
		}

		// Remove authorization header to prevent it from being forwarded
		r.Header.Del("Authorization")

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func extractStringSlice(raw any) []string {
	var result []string
	switch val := raw.(type) {
	case []any:
		for _, item := range val {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
	case []string:
		result = val
	}
	return result
}

// globToRegex converts a Casbin keyMatch2-style pattern into a
// PromQL regex. `*` matches any sequence, `?` matches a single character.
// A bare `*` (e.g. from the `cluster/*` or `*/*` policies) becomes `.+` which
// matches any non-empty value.
func globToRegex(pattern string) string {
	if pattern == "*" {
		return ".+"
	}
	var b strings.Builder
	for _, ch := range pattern {
		switch ch {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(ch)))
		}
	}
	return b.String()
}

// resourcePart selects which part of an authorized cluster/namespace pair an
// extractor enforces.
type resourcePart int

const (
	partNamespace resourcePart = iota
	partCluster
)

// contextLabelExtractor extracts label values from allowed resources
// determined by Casbin authorization and implements the injectproxy.ExtractLabeler
// interface. One extractor instance is created per enforced label (cluster
// and namespace); each instance is mounted on its own prom-label-proxy
// route set (see newPrometheusProxy).
type contextLabelExtractor struct {
	label string
	part  resourcePart
}

func encodeAllowedPairs(pairs [][2]string) (string, error) {
	raw := make([][]string, 0, len(pairs))
	for _, p := range pairs {
		raw = append(raw, []string{p[0], p[1]})
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func decodeAllowedPairs(encoded string) ([][2]string, error) {
	var raw [][]string
	if err := json.Unmarshal([]byte(encoded), &raw); err != nil {
		return nil, err
	}
	pairs := make([][2]string, 0, len(raw))
	for _, r := range raw {
		if len(r) != 2 {
			return nil, fmt.Errorf("invalid authorized resource pair %v", r)
		}
		pairs = append(pairs, [2]string{r[0], r[1]})
	}
	return pairs, nil
}

// allowedPairsFromRequest returns the authorized cluster/namespace pairs. In
// dual-label mode the pairs arrive via the internal header set by the outer
// (cluster) extractor; otherwise they come from the request context, which
// the authz middleware populates.
func allowedPairsFromRequest(r *http.Request) ([][2]string, bool) {
	if encoded := r.Header.Get(internalAuthorizedResourcesHeader); encoded != "" {
		pairs, err := decodeAllowedPairs(encoded)
		if err != nil {
			log.Printf("[label-injection] failed to decode authorized pairs: %v", err)
			return nil, false
		}
		return pairs, true
	}
	pairs, ok := r.Context().Value(ContextKeyAllowedClusters).([][2]string)
	return pairs, ok
}

// ExtractLabel implements the ExtractLabeler interface by extracting the
// cluster or namespace part (depending on the configured part) of each
// authorized resource, translating globs to regex, and storing the combined
// regex union in the request context for prom-label-proxy.
func (e *contextLabelExtractor) ExtractLabel(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pairs, ok := allowedPairsFromRequest(r)
		if !ok || len(pairs) == 0 {
			http.Error(w, "No authorized namespaces", http.StatusForbidden)
			return
		}

		// The outer (cluster) extractor passes the authorized pairs to the
		// inner (namespace) extractor over the internal proxy hop, where the
		// request context does not survive. The inner extractor removes the
		// header again so it never reaches the upstream.
		if e.part == partCluster {
			if encoded, err := encodeAllowedPairs(pairs); err == nil {
				r.Header.Set(internalAuthorizedResourcesHeader, encoded)
			} else {
				log.Printf("[label-injection] failed to encode authorized pairs: %v", err)
			}
		} else {
			r.Header.Del(internalAuthorizedResourcesHeader)
		}

		// Translate each authorized resource's selected part into a regex.
		// The authz middleware already sorted the pairs, so the result is
		// deterministic. Identical regexes (e.g. the same namespace name
		// authorized in multiple clusters) are deduplicated.
		regexes := make([]string, 0, len(pairs))
		seen := map[string]bool{}
		for _, pair := range pairs {
			part := pair[1]
			if e.part == partCluster {
				part = pair[0]
			}
			re := globToRegex(part)
			if !seen[re] {
				seen[re] = true
				regexes = append(regexes, re)
			}
		}
		sort.Strings(regexes)
		labelValue := strings.Join(regexes, "|")

		log.Printf("[label-injection] Injecting %s regex: %s", e.label, labelValue)

		// Store label in context using prom-label-proxy's WithLabelValues
		ctx := injectproxy.WithLabelValues(r.Context(), []string{labelValue})
		next(w, r.WithContext(ctx))
	})
}

// newPrometheusProxy creates a prom-label-proxy handler for Prometheus
// datasources that enforces the tenant's cluster and namespace labels.
//
// In dual-label mode (clusterLabel != "") two prom-label-proxy instances are
// chained: an outer proxy enforces the cluster label against an internal
// loopback listener that serves an inner proxy enforcing the namespace label
// against the real upstream. The authorized cluster/namespace pairs are
// carried from the outer to the inner extractor via an internal header.
//
// The returned cleanup function shuts down the internal listener and should
// be called when the proxy is no longer needed (e.g. in tests).
func newPrometheusProxy(upstreamURL, namespaceLabel, clusterLabel string) (http.Handler, func(), error) {
	upstream, err := url.Parse(upstreamURL)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse upstream URL: %w", err)
	}

	// WithRegexMatch makes the injected label a regex matcher, which is how
	// wildcard policies (dev-* -> dev-.*) and multi-namespace access
	// (ns1|ns2) are enforced.
	// WithEnabledLabelsAPI turns on /api/v1/labels and /api/v1/label/<n>/values.
	opts := []injectproxy.Option{
		injectproxy.WithRegexMatch(),
		injectproxy.WithEnabledLabelsAPI(),
	}

	if clusterLabel == "" {
		// Single-label mode: only the namespace label is enforced.
		extractor := &contextLabelExtractor{label: namespaceLabel, part: partNamespace}
		routes, err := injectproxy.NewRoutes(upstream, namespaceLabel, extractor, opts...)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create proxy routes: %w", err)
		}
		return routes, func() {}, nil
	}

	innerExtractor := &contextLabelExtractor{label: namespaceLabel, part: partNamespace}
	innerRoutes, err := injectproxy.NewRoutes(upstream, namespaceLabel, innerExtractor, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create namespace proxy routes: %w", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create internal listener: %w", err)
	}
	internalServer := &http.Server{Handler: innerRoutes}
	go func() {
		if err := internalServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("internal namespace proxy server error: %v", err)
		}
	}()

	internalURL := &url.URL{Scheme: "http", Host: listener.Addr().String()}

	outerExtractor := &contextLabelExtractor{label: clusterLabel, part: partCluster}
	outerRoutes, err := injectproxy.NewRoutes(internalURL, clusterLabel, outerExtractor, opts...)
	if err != nil {
		_ = internalServer.Close()
		return nil, nil, fmt.Errorf("failed to create cluster proxy routes: %w", err)
	}

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = internalServer.Shutdown(ctx)
	}
	return outerRoutes, cleanup, nil
}
