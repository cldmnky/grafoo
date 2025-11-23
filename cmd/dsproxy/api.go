package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type APIHandler struct {
	client client.Client
}

func NewAPIHandler(client client.Client) *APIHandler {
	return &APIHandler{client: client}
}

func (h *APIHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/rules", h.handleRules)
	mux.HandleFunc("/api/v1/rules/", h.handleRule) // For DELETE/PUT
}

func (h *APIHandler) handleRules(w http.ResponseWriter, r *http.Request) {
	if h.client == nil {
		http.Error(w, "Kubernetes client not initialized", http.StatusServiceUnavailable)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.listRules(w, r)
	case http.MethodPost:
		h.createRule(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *APIHandler) handleRule(w http.ResponseWriter, r *http.Request) {
	if h.client == nil {
		http.Error(w, "Kubernetes client not initialized", http.StatusServiceUnavailable)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/v1/rules/")
	if name == "" {
		http.Error(w, "Rule name required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodDelete:
		h.deleteRule(w, r, name)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *APIHandler) listRules(w http.ResponseWriter, r *http.Request) {
	var rules grafoov1alpha1.GrafanaDataSourceRuleList
	if err := h.client.List(r.Context(), &rules); err != nil {
		http.Error(w, fmt.Sprintf("Failed to list rules: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rules.Items)
}

func (h *APIHandler) createRule(w http.ResponseWriter, r *http.Request) {
	var rule grafoov1alpha1.GrafanaDataSourceRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Ensure namespace is set (default to current namespace or specific one?)
	// For now, let's assume the client sends the namespace or we default to "default"
	if rule.Namespace == "" {
		rule.Namespace = "default" // TODO: Make configurable or extract from context
	}

	if err := h.client.Create(r.Context(), &rule); err != nil {
		http.Error(w, fmt.Sprintf("Failed to create rule: %v", err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(rule)
}

func (h *APIHandler) deleteRule(w http.ResponseWriter, r *http.Request, name string) {
	// We need namespace to delete. For now assume "default" or pass via query param?
	// Better: pass namespace in query param ?namespace=...
	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "default"
	}

	rule := &grafoov1alpha1.GrafanaDataSourceRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	if err := h.client.Delete(r.Context(), rule); err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete rule: %v", err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
