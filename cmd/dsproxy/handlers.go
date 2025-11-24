package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"

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
		auds, err := claims.GetAudience()
		if err != nil {
			log.Printf("Failed to get audience from claims: %v", err)
			http.Error(w, "Invalid audience claim", http.StatusUnauthorized)
			return
		}

		validAudience := false
		for _, a := range auds {
			if a == f_jwtAudience {
				validAudience = true
				break
			}
		}

		if !validAudience {
			log.Printf("Invalid audience. Expected %s, got %v", f_jwtAudience, auds)
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

		// Use email as primary identity if available, otherwise sub
		identity := sub
		if email != "" {
			identity = email
		}

		ctx := context.WithValue(r.Context(), ContextKeyEmail, identity)
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
		namespaces := make([]string, 0, len(allowedPairs))
		hasWildcard := false
		for _, pair := range allowedPairs {
			namespace := pair[1] // pair is [cluster, namespace]
			if namespace == "*" {
				hasWildcard = true
				break
			}
			namespaces = append(namespaces, namespace)
		}

		// If wildcard access is granted, use a regex that matches all namespaces
		// Use .+ to match any non-empty namespace (not .* to avoid matching metrics without namespace label)
		if hasWildcard {
			log.Printf("[label-injection] Wildcard namespace access detected, using regex matcher")
			ctx := injectproxy.WithLabelValues(r.Context(), []string{".+"})
			next(w, r.WithContext(ctx))
			return
		}

		if len(namespaces) == 0 {
			http.Error(w, "No authorized namespaces found", http.StatusForbidden)
			return
		}

		log.Printf("[label-injection] Injecting namespaces: %v", namespaces)

		// For single namespace, use it directly
		// For multiple namespaces, create a regex pattern
		var labelValue string
		if len(namespaces) == 1 {
			labelValue = namespaces[0]
		} else {
			// Multiple namespaces: use regex with alternation
			// prom-label-proxy will create namespace=~"ns1|ns2|ns3"
			labelValue = strings.Join(namespaces, "|")
			log.Printf("[label-injection] Multiple namespaces authorized, using regex: %s", labelValue)
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

	routes, err := injectproxy.NewRoutes(
		upstream,
		label,
		extractor,
		injectproxy.WithEnabledLabelsAPI(),
		injectproxy.WithRegexMatch(), // Enable regex matching for namespace patterns
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy routes: %w", err)
	}

	return routes, nil
}

type DynamicProxy struct {
	label    string
	scheme   string
	handlers map[string]http.Handler
	mu       sync.RWMutex
}

func NewDynamicProxy(scheme, label string) *DynamicProxy {
	return &DynamicProxy{
		label:    label,
		scheme:   scheme,
		handlers: make(map[string]http.Handler),
	}
}

func (p *DynamicProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if host == "" {
		http.Error(w, "Missing Host header", http.StatusBadRequest)
		return
	}

	p.mu.RLock()
	handler, exists := p.handlers[host]
	p.mu.RUnlock()

	if !exists {
		p.mu.Lock()
		defer p.mu.Unlock()
		// Double check: verify handler doesn't exist after acquiring write lock
		if handler, exists = p.handlers[host]; !exists {
			var err error
			upstreamURL := fmt.Sprintf("%s://%s", p.scheme, host)
			handler, err = newPrometheusProxy(upstreamURL, p.label)
			if err != nil {
				log.Printf("Failed to create proxy for %s: %v", upstreamURL, err)
				http.Error(w, "Failed to create proxy", http.StatusInternalServerError)
				return
			}
			p.handlers[host] = handler
		}
	}

	handler.ServeHTTP(w, r)
}
