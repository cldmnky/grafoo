package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"time"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/persist"
	fileadapter "github.com/casbin/casbin/v2/persist/file-adapter"
	"github.com/fsnotify/fsnotify"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type AuthzService struct {
	Enforcer    *casbin.Enforcer
	policyFile  string
	policyModel string
	k8sClient   client.Client
}

func NewAuthzService(ctx context.Context, policyPath string, k8sClient client.Client) (*AuthzService, error) {
	policyFile := filepath.Join(policyPath, "policy.csv")
	policyModel := filepath.Join(policyPath, "model.conf")

	var adapter persist.Adapter
	if k8sClient != nil {
		adapter = NewK8sAdapter(k8sClient)
	} else {
		adapter = fileadapter.NewAdapter(policyFile)
	}

	enforcer, err := casbin.NewEnforcer(policyModel, adapter)
	if err != nil {
		return nil, fmt.Errorf("failed to create enforcer: %w", err)
	}
	if err := enforcer.LoadPolicy(); err != nil {
		return nil, fmt.Errorf("failed to load policy: %w", err)
	}

	authz := &AuthzService{Enforcer: enforcer, policyFile: policyFile, policyModel: policyModel, k8sClient: k8sClient}
	go authz.watchPolicyAndModel(ctx)
	return authz, nil
}

func (a *AuthzService) Authorize(subject string, groups []string, domain, cluster, namespace, action string) bool {
	resource := fmt.Sprintf("%s/%s", cluster, namespace)

	if ok, _ := a.Enforcer.Enforce(subject, domain, resource, action); ok {
		return true
	}
	for _, group := range groups {
		if ok, _ := a.Enforcer.Enforce(group, domain, resource, action); ok {
			return true
		}
	}
	return false
}

func (a *AuthzService) watchPolicyAndModel(ctx context.Context) {
	if a.k8sClient != nil {
		// Polling for K8s
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Println("[authz] stopping k8s policy watcher")
				return
			case <-ticker.C:
				if err := a.Enforcer.LoadPolicy(); err != nil {
					log.Printf("[authz] failed to reload policy from k8s: %v", err)
				}
			}
		}
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("[authz] failed to initialize watcher: %v", err)
		return
	}
	defer watcher.Close()

	for _, file := range []string{a.policyModel, a.policyFile} {
		if err := watcher.Add(file); err != nil {
			log.Printf("[authz] failed to watch file %s: %v", file, err)
		}
		log.Printf("[authz] watching file: %s", file)
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("[authz] stopping policy watcher")
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 && (strings.HasSuffix(event.Name, ".csv") || strings.HasSuffix(event.Name, ".conf")) {
				log.Println("[authz] detected policy or model change, reloading...")
				if err := a.Enforcer.LoadPolicy(); err != nil {
					log.Printf("[authz] failed to reload policy: %v", err)
				} else {
					log.Println("[authz] policy reloaded")
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("[authz] watcher error: %v", err)
		}
	}
}

func (a *AuthzService) authzMiddleware(action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			email, _ := ctx.Value(ContextKeyEmail).(string)
			groups, _ := ctx.Value(ContextKeyGroups).([]string)
			datasourceID, _ := ctx.Value(ContextDataSourceID).(string)

			// Get all policies and check which resources the user can access
			allPolicies, err := a.Enforcer.GetPolicy()
			if err != nil {
				log.Printf("[authz] error getting policies: %v", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			subjects := append(groups, email)
			allowed := map[string]struct{}{}

			// Check each policy to see if it applies to our subjects and datasource
			for _, policy := range allPolicies {
				if len(policy) < 4 {
					continue
				}

				policySub := policy[0]
				policyDom := policy[1]
				policyObj := policy[2]
				policyAct := policy[3]

				// Check if action matches
				if policyAct != action {
					continue
				}

				// Check if datasource matches (handle wildcards)
				datasourceMatches := policyDom == "*" || policyDom == datasourceID
				if !datasourceMatches {
					continue
				}

				// Check if subject matches (direct or via role)
				subjectMatches := false
				for _, sub := range subjects {
					if policySub == sub {
						subjectMatches = true
						break
					}
					// Check role inheritance
					if ok, _ := a.Enforcer.HasRoleForUser(sub, policySub); ok {
						subjectMatches = true
						break
					}
				}

				if subjectMatches {
					// This policy grants access to this resource
					log.Printf("[authz] allowing resource %s for subject %s", policyObj, policySub)
					allowed[policyObj] = struct{}{}
				}
			}

			if len(allowed) == 0 {
				log.Printf("[authz] no resources allowed for %s with datasource %s", email, datasourceID)
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			// Convert to list of [cluster, namespace]
			var allowedPairs [][2]string
			for res := range allowed {
				parts := strings.SplitN(res, "/", 2)
				if len(parts) == 2 {
					allowedPairs = append(allowedPairs, [2]string{parts[0], parts[1]})
				}
			}

			log.Printf("[authz] allowed cluster/namespace pairs: %v", allowedPairs)
			ctx = context.WithValue(ctx, ContextKeyAllowedClusters, allowedPairs)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
