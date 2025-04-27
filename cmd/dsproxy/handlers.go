package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const (
	ContextKeyEmail     contextKey = "email"
	ContextKeyGroups    contextKey = "groups"
	ContextDataSourceID contextKey = "datasourceID"
)

func proxyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Create a new request to the target using the Host header and original URI
		log.Println("Proxying request:", r.Host, r.Method, r.URL.RequestURI())

		// Determine scheme based on whether the request is over TLS
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		target := fmt.Sprintf("%s://%s%s", scheme, r.Host, r.URL.RequestURI())
		req, err := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
		if err != nil {
			http.Error(w, "Failed to create proxy request", http.StatusInternalServerError)
			return
		}

		// Copy headers
		req.Header = r.Header.Clone()
		req.Host = r.Host

		// Use http.DefaultTransport for outgoing request
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			http.Error(w, "Proxy error: "+err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		// Copy response headers
		for k, vv := range resp.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		// Copy response body
		_, _ = io.Copy(w, resp.Body)
	})
}

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
		if !ok || aud != "example-app" {
			http.Error(w, "Invalid audience", http.StatusUnauthorized)
			return
		}
		email, _ := claims["email"].(string)
		groups := extractStringSlice(claims["groups"])

		log.Printf("Authenticated user: %s, groups: %v", email, groups)
		ctx := context.WithValue(r.Context(), ContextKeyEmail, email)
		ctx = context.WithValue(ctx, ContextKeyGroups, groups)
		// Get ds from the header X-Datasource-Uid: eeiyrwxf5makgd
		datasourceID := r.Header.Get("X-Datasource-Uid")
		if datasourceID != "" {
			ctx = context.WithValue(ctx, ContextDataSourceID, datasourceID)
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
