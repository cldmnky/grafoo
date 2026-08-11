package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
)

//go:embed ui/dist
var uiAssets embed.FS

type PolicyData struct {
	Policies         [][]string `json:"policies"`
	GroupingPolicies [][]string `json:"groupingPolicies"`
}

const (
	maxPolicyRows = 10000
)

func validatePolicyData(data *PolicyData) error {
	if len(data.Policies) > maxPolicyRows || len(data.GroupingPolicies) > maxPolicyRows {
		return fmt.Errorf("too many policy rows (max %d)", maxPolicyRows)
	}
	for i, p := range data.Policies {
		if len(p) != 4 {
			return fmt.Errorf("policy %d must have 4 fields (subject, domain, object, action), got %d", i, len(p))
		}
		for _, f := range p {
			if strings.TrimSpace(f) == "" {
				return fmt.Errorf("policy %d contains an empty field", i)
			}
		}
	}
	for i, g := range data.GroupingPolicies {
		if len(g) != 2 {
			return fmt.Errorf("grouping policy %d must have 2 fields (user, role), got %d", i, len(g))
		}
		for _, f := range g {
			if strings.TrimSpace(f) == "" {
				return fmt.Errorf("grouping policy %d contains an empty field", i)
			}
		}
	}
	return nil
}

func policyAPIHandler(authz *AuthzService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			authz.mu.RLock()
			policies, err := authz.Enforcer.GetPolicy()
			if err != nil {
				authz.mu.RUnlock()
				http.Error(w, fmt.Sprintf("Failed to get policies: %v", err), http.StatusInternalServerError)
				return
			}
			groupingPolicies, err := authz.Enforcer.GetGroupingPolicy()
			authz.mu.RUnlock()
			if err != nil {
				http.Error(w, fmt.Sprintf("Failed to get grouping policies: %v", err), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(PolicyData{
				Policies:         policies,
				GroupingPolicies: groupingPolicies,
			})
			return
		}

		if r.Method == http.MethodPost {
			var data PolicyData
			if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := validatePolicyData(&data); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			if err := replacePolicy(authz, &data); err != nil {
				http.Error(w, fmt.Sprintf("Failed to save policy: %v", err), http.StatusInternalServerError)
				return
			}

			w.WriteHeader(http.StatusOK)
			return
		}

		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// replacePolicy atomically replaces the policy file and then reloads the
// enforcer from it. The file is written first (temp file + rename, so the
// policy is never left half-written) and only then is the in-memory state
// reloaded. If anything fails, the previous file content is restored and the
// enforcer reloaded, so a failed save never leaves an empty or partial policy.
func replacePolicy(authz *AuthzService, data *PolicyData) error {
	oldContent, err := os.ReadFile(authz.policyFile)
	if err != nil {
		return fmt.Errorf("failed to read current policy file: %w", err)
	}

	newContent := serializePolicy(data)

	tmpFile := authz.policyFile + ".tmp"
	if err := os.WriteFile(tmpFile, newContent, 0644); err != nil {
		return fmt.Errorf("failed to write policy file: %w", err)
	}
	if err := os.Rename(tmpFile, authz.policyFile); err != nil {
		return fmt.Errorf("failed to replace policy file: %w", err)
	}

	authz.mu.Lock()
	defer authz.mu.Unlock()

	if err := authz.Enforcer.LoadModel(); err != nil {
		restorePolicy(authz, oldContent)
		return fmt.Errorf("failed to reload model: %w", err)
	}
	if err := authz.Enforcer.LoadPolicy(); err != nil {
		restorePolicy(authz, oldContent)
		return fmt.Errorf("failed to reload policy: %w", err)
	}
	return nil
}

// serializePolicy writes the policy rows in Casbin CSV format.
func serializePolicy(data *PolicyData) []byte {
	var b strings.Builder
	for _, p := range data.Policies {
		b.WriteString("p, " + strings.Join(p, ", ") + "\n")
	}
	for _, g := range data.GroupingPolicies {
		b.WriteString("g, " + strings.Join(g, ", ") + "\n")
	}
	return []byte(b.String())
}

func restorePolicy(authz *AuthzService, oldContent []byte) {
	log.Printf("[authz] policy save failed, rolling back to previous policy")
	if err := os.WriteFile(authz.policyFile, oldContent, 0644); err != nil {
		log.Printf("[authz] failed to restore policy file: %v", err)
	}
	if err := authz.Enforcer.LoadModel(); err != nil {
		log.Printf("[authz] failed to reload model during rollback: %v", err)
	}
	if err := authz.Enforcer.LoadPolicy(); err != nil {
		log.Printf("[authz] failed to reload policy during rollback: %v", err)
	}
}

// newUIHandler builds the UI HTTP handler: an authenticated policy API plus
// the embedded static UI assets.
func newUIHandler(authz *AuthzService) http.Handler {
	distFS, err := fs.Sub(uiAssets, "ui/dist")
	if err != nil {
		log.Fatalf("Failed to load UI assets: %v", err)
	}

	mux := http.NewServeMux()
	fileServer := http.FileServer(http.FS(distFS))

	// API Endpoints - the policy API requires a valid JWT, same as the
	// proxy endpoints. The UI is intended to be accessed via port-forward
	// or behind an authenticated proxy that injects the Authorization header.
	mux.Handle("/api/policy", authMiddleware(policyAPIHandler(authz)))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Unknown API routes are not SPA routes; return 404 instead of
		// serving index.html.
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		// Check if the requested file exists in the file system
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		_, err := distFS.Open(path)
		if err != nil {
			// If file not found, serve index.html (SPA routing)
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})

	return mux
}

// startUIServer serves the policy management UI. The server binds to
// 127.0.0.1 so it is not exposed on the network unless explicitly
// port-forwarded or proxied.
func startUIServer(addr string, authz *AuthzService) *http.Server {
	server := &http.Server{
		Addr:    addr,
		Handler: newUIHandler(authz),
	}

	log.Printf("Starting UI server on %s", addr)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("UI server error: %v", err)
		}
	}()

	return server
}
