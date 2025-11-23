package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"

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
		// check aud
		aud, ok := claims["aud"].(string)
		if !ok || aud != f_jwtAudience {
			http.Error(w, "Invalid audience", http.StatusUnauthorized)
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

		// Extract namespaces from the allowed pairs
		// For now, we'll use the first allowed namespace
		// TODO: Support multiple namespaces via regex in prom-label-proxy
		namespaces := make([]string, 0, len(allowedPairs))
		for _, pair := range allowedPairs {
			namespace := pair[1] // pair is [cluster, namespace]
			namespaces = append(namespaces, namespace)
		}

		if len(namespaces) == 0 {
			http.Error(w, "No authorized namespaces found", http.StatusForbidden)
			return
		}

		log.Printf("[label-injection] Injecting namespaces: %v", namespaces)

		// For single namespace, use it directly
		// For multiple namespaces, we need to handle OR logic (future enhancement)
		labelValue := namespaces[0]
		if len(namespaces) > 1 {
			// TODO: prom-label-proxy supports regex - could inject namespace=~"ns1|ns2|ns3"
			log.Printf("[label-injection] Warning: Multiple namespaces authorized, using first: %s", labelValue)
		}

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

	routes, err := injectproxy.NewRoutes(upstream, label, extractor)
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy routes: %w", err)
	}

	return routes, nil
}
