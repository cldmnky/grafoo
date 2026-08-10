package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

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

// globToRegex converts a Casbin keyMatch2-style namespace pattern into a
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

// contextLabelExtractor extracts namespace labels from allowed resources
// determined by Casbin authorization and implements the injectproxy.ExtractLabeler interface
type contextLabelExtractor struct {
	label string
}

// ExtractLabel implements the ExtractLabeler interface by extracting allowed namespaces from context
// The authz middleware populates ContextKeyAllowedClusters with authorized cluster/namespace pairs
func (e *contextLabelExtractor) ExtractLabel(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get allowed cluster/namespace pairs from authz middleware
		allowedPairs, ok := r.Context().Value(ContextKeyAllowedClusters).([][2]string)
		if !ok || len(allowedPairs) == 0 {
			http.Error(w, "No authorized namespaces", http.StatusForbidden)
			return
		}

		// Translate each authorized resource's namespace part into a regex.
		// The authz middleware already sorted the pairs, so the result is
		// deterministic. All authorized namespaces are injected as a single
		// regex (e.g. namespace=~"monitoring|alerting") via the proxy's
		// regex match mode. Identical regexes (e.g. the same namespace name
		// authorized in multiple clusters) are deduplicated.
		regexes := make([]string, 0, len(allowedPairs))
		seen := map[string]bool{}
		for _, pair := range allowedPairs {
			re := globToRegex(pair[1])
			if !seen[re] {
				seen[re] = true
				regexes = append(regexes, re)
			}
		}
		sort.Strings(regexes)
		labelValue := strings.Join(regexes, "|")

		log.Printf("[label-injection] Injecting namespace regex: %s", labelValue)

		// Store label in context using prom-label-proxy's WithLabelValues
		ctx := injectproxy.WithLabelValues(r.Context(), []string{labelValue})
		next(w, r.WithContext(ctx))
	})
}

// newPrometheusProxy creates a prom-label-proxy handler for Prometheus datasources
func newPrometheusProxy(upstreamURL string, label string) (http.Handler, error) {
	upstream, err := url.Parse(upstreamURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse upstream URL: %w", err)
	}

	extractor := &contextLabelExtractor{
		label: label,
	}

	// WithRegexMatch makes the injected label a regex matcher, which is how
	// wildcard policies (dev-* -> dev-.*) and multi-namespace access
	// (ns1|ns2) are enforced.
	// WithEnabledLabelsAPI turns on /api/v1/labels and /api/v1/label/<n>/values.
	routes, err := injectproxy.NewRoutes(upstream, label, extractor,
		injectproxy.WithRegexMatch(),
		injectproxy.WithEnabledLabelsAPI(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy routes: %w", err)
	}

	return routes, nil
}
