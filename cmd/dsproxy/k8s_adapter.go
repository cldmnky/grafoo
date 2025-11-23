package main

import (
	"context"
	"fmt"

	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type K8sAdapter struct {
	client client.Client
}

func NewK8sAdapter(client client.Client) *K8sAdapter {
	return &K8sAdapter{client: client}
}

// LoadPolicy loads all policy rules from the storage.
func (a *K8sAdapter) LoadPolicy(model model.Model) error {
	ctx := context.Background()
	var rules grafoov1alpha1.GrafanaDataSourceRuleList
	if err := a.client.List(ctx, &rules); err != nil {
		return fmt.Errorf("failed to list GrafanaDataSourceRules: %w", err)
	}

	for _, rule := range rules.Items {
		for _, perm := range rule.Spec.Permissions {
			// Policy format: p, sub, dom, obj, act
			// sub: user or group
			// dom: datasourceID
			// obj: resource (cluster/namespace)
			// act: action

			subjects := []string{}
			if rule.Spec.User != "" {
				subjects = append(subjects, rule.Spec.User)
			}
			if rule.Spec.Group != "" {
				subjects = append(subjects, rule.Spec.Group)
			}

			for _, sub := range subjects {
				line := fmt.Sprintf("p, %s, %s, %s, %s", sub, rule.Spec.DataSourceID, perm.Resource, perm.Action)
				persist.LoadPolicyLine(line, model)
			}
		}
	}
	return nil
}

// SavePolicy saves all policy rules to the storage.
func (a *K8sAdapter) SavePolicy(model model.Model) error {
	return fmt.Errorf("not implemented: use Kubernetes API to manage rules")
}

// AddPolicy adds a policy rule to the storage.
func (a *K8sAdapter) AddPolicy(sec string, ptype string, rule []string) error {
	return fmt.Errorf("not implemented: use Kubernetes API to manage rules")
}

// RemovePolicy removes a policy rule from the storage.
func (a *K8sAdapter) RemovePolicy(sec string, ptype string, rule []string) error {
	return fmt.Errorf("not implemented: use Kubernetes API to manage rules")
}

// RemoveFilteredPolicy removes policy rules that match the filter from the storage.
func (a *K8sAdapter) RemoveFilteredPolicy(sec string, ptype string, fieldIndex int, fieldValues ...string) error {
	return fmt.Errorf("not implemented: use Kubernetes API to manage rules")
}
