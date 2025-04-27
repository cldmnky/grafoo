package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/casbin/casbin/v2"
	fileadapter "github.com/casbin/casbin/v2/persist/file-adapter"
	"github.com/fsnotify/fsnotify"
)

const (
	ContextKeyAllowedClusters = contextKey("allowed_clusters")
)

type AuthzService struct {
	Enforcer    *casbin.Enforcer
	policyFile  string
	policyModel string
}

func NewAuthzService(policyPath string) (*AuthzService, error) {
	policyFile := filepath.Join(policyPath, "policy.csv")
	policyModel := filepath.Join(policyPath, "model.conf")
	adapter := fileadapter.NewAdapter(policyFile)
	enforcer, err := casbin.NewEnforcer(policyModel, adapter)
	if err != nil {
		return nil, fmt.Errorf("failed to create enforcer: %w", err)
	}
	if err := enforcer.LoadPolicy(); err != nil {
		return nil, fmt.Errorf("failed to load policy: %w", err)
	}

	authz := &AuthzService{Enforcer: enforcer, policyFile: policyFile, policyModel: policyModel}
	go authz.watchPolicyAndModel()
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

func (a *AuthzService) watchPolicyAndModel() {
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

			subjects := append(groups, email)
			allowed := map[string]struct{}{}
			for _, sub := range subjects {
				policies, err := a.Enforcer.GetFilteredPolicy(0, sub, datasourceID, "", action)
				log.Printf("[authz] policies for subject %s: %v", sub, policies)
				if err != nil {
					log.Printf("[authz] error getting filtered policy for subject %s: %v", sub, err)
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
					return
				}
				for _, policy := range policies {
					resource := policy[2] // obj, like cluster1/namespaceX
					allowed[resource] = struct{}{}
				}
			}

			if len(allowed) == 0 {
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

			ctx = context.WithValue(ctx, ContextKeyAllowedClusters, allowedPairs)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
