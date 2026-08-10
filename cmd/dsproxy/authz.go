package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/casbin/casbin/v2"
	fileadapter "github.com/casbin/casbin/v2/persist/file-adapter"
	"github.com/casbin/casbin/v2/util"
	"github.com/fsnotify/fsnotify"
)

// AuthzService wraps the Casbin enforcer with a mutex because the enforcer is
// not safe for concurrent reads while the policy/model is being reloaded by
// the file watcher or the policy management API.
type AuthzService struct {
	mu          sync.RWMutex
	Enforcer    *casbin.Enforcer
	policyFile  string
	policyModel string
}

func NewAuthzService(ctx context.Context, policyPath string) (*AuthzService, error) {
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
	go authz.watchPolicyAndModel(ctx)
	return authz, nil
}

func (a *AuthzService) Authorize(subject string, groups []string, domain, cluster, namespace, action string) bool {
	resource := fmt.Sprintf("%s/%s", cluster, namespace)

	a.mu.RLock()
	defer a.mu.RUnlock()

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

// reload reloads both the model and the policy from disk. It is invoked by
// the file watcher whenever policy.csv or model.conf changes.
func (a *AuthzService) reload() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.Enforcer.LoadModel(); err != nil {
		log.Printf("[authz] failed to reload model: %v", err)
		return
	}
	if err := a.Enforcer.LoadPolicy(); err != nil {
		log.Printf("[authz] failed to reload policy: %v", err)
		return
	}
	log.Println("[authz] policy and model reloaded")
}

// watchPolicyAndModel watches the policy directory so that both in-place
// edits and atomic file replacement (rename) are detected. Changes to
// model.conf reload the model; changes to policy.csv reload the policy.
func (a *AuthzService) watchPolicyAndModel(ctx context.Context) {
	dir := filepath.Dir(a.policyFile)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("[authz] failed to initialize watcher: %v", err)
		return
	}
	defer watcher.Close()

	if err := watcher.Add(dir); err != nil {
		log.Printf("[authz] failed to watch directory %s: %v", dir, err)
		return
	}
	log.Printf("[authz] watching policy directory: %s", dir)

	var timer *time.Timer
	for {
		select {
		case <-ctx.Done():
			log.Println("[authz] stopping policy watcher")
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			name := filepath.Base(event.Name)
			if name != filepath.Base(a.policyFile) && name != filepath.Base(a.policyModel) {
				continue
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
				continue
			}
			log.Printf("[authz] detected change in %s, scheduling reload...", name)
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(100*time.Millisecond, a.reload)
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

			sub, _ := ctx.Value(ContextKeyEmail).(string)
			groups, _ := ctx.Value(ContextKeyGroups).([]string)
			datasourceID, _ := ctx.Value(ContextDataSourceID).(string)

			allowed := a.allowedResources(sub, groups, datasourceID, action)

			if len(allowed) == 0 {
				log.Printf("[authz] no resources allowed for %s with datasource %s", sub, datasourceID)
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			// Convert to list of [cluster, namespace], sorted so that label
			// injection is deterministic across requests.
			var allowedPairs [][2]string
			for res := range allowed {
				parts := strings.SplitN(res, "/", 2)
				if len(parts) == 2 {
					allowedPairs = append(allowedPairs, [2]string{parts[0], parts[1]})
				}
			}
			sort.Slice(allowedPairs, func(i, j int) bool {
				if allowedPairs[i][0] != allowedPairs[j][0] {
					return allowedPairs[i][0] < allowedPairs[j][0]
				}
				return allowedPairs[i][1] < allowedPairs[j][1]
			})

			log.Printf("[authz] allowed cluster/namespace pairs: %v", allowedPairs)
			ctx = context.WithValue(ctx, ContextKeyAllowedClusters, allowedPairs)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// allowedResources returns the set of policy objects (cluster/namespace)
// that any of the subjects (directly or via role inheritance) can access for
// the given datasource and action.
func (a *AuthzService) allowedResources(sub string, groups []string, datasourceID, action string) map[string]struct{} {
	// Get all policies and check which resources the user can access
	a.mu.RLock()
	defer a.mu.RUnlock()

	allPolicies, err := a.Enforcer.GetPolicy()
	if err != nil {
		log.Printf("[authz] error getting policies: %v", err)
		return nil
	}

	subjects := append(groups, sub)
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

		// Check if datasource matches (supports wildcards via keyMatch2)
		datasourceMatches := policyDom == "*" || util.KeyMatch2(datasourceID, policyDom)
		if !datasourceMatches {
			continue
		}

		// Check if subject matches (direct or via role)
		subjectMatches := false
		for _, s := range subjects {
			if policySub == s {
				subjectMatches = true
				break
			}
			// Check role inheritance
			if ok, _ := a.Enforcer.HasRoleForUser(s, policySub); ok {
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
	return allowed
}
