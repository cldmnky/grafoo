package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	grafanav1beta1 "github.com/grafana/grafana-operator/v5/api/v1beta1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
)

func (r *GrafanaReconciler) ReconcileDataSources(ctx context.Context, instance *grafoov1alpha1.Grafana, needsRefresh bool) error {
	logger := log.FromContext(ctx)
	var token string
	if needsRefresh {
		request := &authenticationv1.TokenRequest{
			Spec: authenticationv1.TokenRequestSpec{
				Audiences:         []string{"grafoo"},
				ExpirationSeconds: int64Ptr(int64(instance.Spec.TokenDuration.Duration.Seconds())),
			},
		}
		resp, err := r.Clientset.CoreV1().ServiceAccounts(instance.Namespace).CreateToken(ctx, r.generateNameForComponent(instance, "sa"), request, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		logger.Info("Created token for datasources", "token expiration", resp.Status.ExpirationTimestamp.Time)
		token = resp.Status.Token
	}
	/*gvr := schema.GroupVersionResource{
		Group:    "loki.grafana.com",
		Version:  "v1",
		Resource: "lokistacks",
	}
	l, err := r.Dynamic.Resource(gvr).Namespace("default").List(context.Background(), v1.ListOptions{})
	if err != nil {
		return err
	}
	logger.Info("Loki stacks", "loki", l)
	*/
	// Get all datasources
	datasources := &grafanav1beta1.GrafanaDatasourceList{}
	dsSelector := labels.SelectorFromSet(labels.Set{
		"app.kubernetes.io/instance": instance.Name,
	})
	err := r.Client.List(ctx, datasources, &client.ListOptions{Namespace: instance.Namespace, LabelSelector: dsSelector})
	if err != nil {
		return err
	}

	var reconciledDatasources = make(map[string]bool)
	for _, ds := range instance.Spec.DataSources {
		if token == "" {
			logger.Info("No token found for datasource", "datasource", ds.Name)
			for _, gds := range datasources.Items {
				if gds.Name == r.generateNameForComponent(instance, ds.GetDataSourceNameHash()) {
					logger.Info("Getting datasource", "name", gds.Name)
					// Extract the token from the existing datasource
					token, err = extractTokenFromSecureJSONData(gds.Spec.Datasource.SecureJSONData)
					if err != nil {
						return err
					}
				}
			}
		}
		switch ds.Type {
		case "prometheus-incluster":
			err = r.reconcilePrometheusDataSource(ctx, instance, ds, token)
			if err != nil {
				return err
			}
			// add the datasource to the list of reconciled datasources
			reconciledDatasources[r.generateNameForComponent(instance, ds.GetDataSourceNameHash())] = true
		case "loki-incluster":
			err = r.reconcileLokiDataSource(ctx, instance, ds, token)
			if err != nil {
				return err
			}
			// add the datasource to the list of reconciled datasources
			reconciledDatasources[r.generateNameForComponent(instance, ds.GetDataSourceNameHash())] = true
		case "tempo-incluster":
			err = r.reconcileTempoDataSource(ctx, instance, ds, token)
			if err != nil {
				return err
			}
			// add the datasource to the list of reconciled datasources
			reconciledDatasources[r.generateNameForComponent(instance, ds.GetDataSourceNameHash())] = true
		case "prometheus-mcoo":
			err = r.reconcilePrometheusDataSource(ctx, instance, ds, token)
			if err != nil {
				return err
			}
			// add the datasource to the list of reconciled datasources
			reconciledDatasources[r.generateNameForComponent(instance, ds.GetDataSourceNameHash())] = true
		default:
			logger.Info("Unknown datasource type", "type", ds.Type)
		}
	}
	for _, gds := range datasources.Items {
		if _, ok := reconciledDatasources[gds.Name]; !ok {
			logger.Info("Deleting datasource", "name", gds.Name)
			err = r.Client.Delete(ctx, &gds)
			if err != nil {
				return err
			}
		}
	}

	// Generate dsproxy configuration based on the reconciled datasources.
	// The dsproxy sidecar needs to know which upstream targets to intercept.
	if err := r.reconcileDSProxyConfig(ctx, instance); err != nil {
		logger.Error(err, "Failed to reconcile dsproxy config")
		return err
	}

	return nil
}

func (r *GrafanaReconciler) reconcilePrometheusDataSource(ctx context.Context, instance *grafoov1alpha1.Grafana, ds grafoov1alpha1.DataSource, token string) error {
	logger := log.FromContext(ctx)
	promDataSource := &grafanav1beta1.GrafanaDatasource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, ds.GetDataSourceNameHash()),
			Namespace: instance.Namespace,
			Labels:    r.generateLabelsForComponent(instance, "grafana"),
		},
	}
	var (
		isDefault      *bool
		secureJSONData json.RawMessage
	)
	if ds.Type == "prometheus-mcoo" {
		isDefault = boolPtr(false)
		secureJSONData = json.RawMessage(`{"httpHeaderValue1": "` + token + `"}`)
	} else {
		isDefault = boolPtr(true)
		secureJSONData = json.RawMessage(`{"httpHeaderValue1": "Bearer ` + token + `"}`)
	}
	promDataSourceSpec := grafanav1beta1.GrafanaDatasourceSpec{
		Datasource: &grafanav1beta1.GrafanaDatasourceInternal{
			Name:           ds.Name,
			Type:           "prometheus",
			Access:         "proxy",
			IsDefault:      isDefault,
			URL:            ds.Prometheus.URL,
			JSONData:       json.RawMessage(`{"httpHeaderName1": "Authorization", "tlsSkipVerify": true}`),
			SecureJSONData: secureJSONData,
		},
		GrafanaCommonSpec: grafanav1beta1.GrafanaCommonSpec{
			InstanceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":      "grafana",
					"app.kubernetes.io/instance":  instance.Name,
					"app.kubernetes.io/component": "grafana",
				},
			},
		},
	}
	op, err := CreateOrUpdateWithRetries(ctx, r.Client, promDataSource, func() error {
		promDataSource.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "prometheus")
		promDataSource.Spec = promDataSourceSpec
		return ctrl.SetControllerReference(instance, promDataSource, r.Scheme)
	})
	if err != nil {
		return err
	}
	if op == ctrlutil.OperationResultCreated {
		logger.Info("Created Prometheus datasource")
	} else if op == ctrlutil.OperationResultUpdated {
		logger.Info("Updated Prometheus datasource")
	}
	return nil
}

func (r *GrafanaReconciler) reconcileLokiDataSource(ctx context.Context, instance *grafoov1alpha1.Grafana, ds grafoov1alpha1.DataSource, token string) error {
	logger := log.FromContext(ctx)
	lokiDataSource := &grafanav1beta1.GrafanaDatasource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, ds.GetDataSourceNameHash()),
			Namespace: instance.Namespace,
			Labels:    r.generateLabelsForComponent(instance, "grafana"),
		},
	}
	lokiDataSourceSpec := grafanav1beta1.GrafanaDatasourceSpec{
		Datasource: &grafanav1beta1.GrafanaDatasourceInternal{
			Name:           ds.Name,
			Type:           "loki",
			Access:         "proxy",
			IsDefault:      boolPtr(false),
			URL:            ds.Loki.URL,
			JSONData:       json.RawMessage(`{"httpHeaderName1": "Authorization", "tlsSkipVerify": true}`),
			SecureJSONData: json.RawMessage(`{"httpHeaderValue1": "Bearer ` + token + `"}`),
		},
		GrafanaCommonSpec: grafanav1beta1.GrafanaCommonSpec{
			InstanceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":      "grafana",
					"app.kubernetes.io/instance":  instance.Name,
					"app.kubernetes.io/component": "grafana",
				},
			},
		},
	}
	op, err := CreateOrUpdateWithRetries(ctx, r.Client, lokiDataSource, func() error {
		lokiDataSource.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "loki")
		lokiDataSource.Spec = lokiDataSourceSpec
		return ctrl.SetControllerReference(instance, lokiDataSource, r.Scheme)
	})
	if err != nil {
		return err
	}
	if op == ctrlutil.OperationResultCreated {
		logger.Info("Created Loki datasource")
	} else if op == ctrlutil.OperationResultUpdated {
		logger.Info("Updated Loki datasource")
	}
	return nil
}

// reconcileTempoDataSource
func (r *GrafanaReconciler) reconcileTempoDataSource(ctx context.Context, instance *grafoov1alpha1.Grafana, ds grafoov1alpha1.DataSource, token string) error {
	logger := log.FromContext(ctx)
	tempoDataSource := &grafanav1beta1.GrafanaDatasource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, ds.GetDataSourceNameHash()),
			Namespace: instance.Namespace,
			Labels:    r.generateLabelsForComponent(instance, "grafana"),
		},
	}
	tempoDataSourceSpec := grafanav1beta1.GrafanaDatasourceSpec{
		Datasource: &grafanav1beta1.GrafanaDatasourceInternal{
			Name:           ds.Name,
			Type:           "tempo",
			Access:         "proxy",
			IsDefault:      boolPtr(false),
			URL:            ds.Tempo.URL,
			JSONData:       json.RawMessage(`{"httpHeaderName1": "Authorization", "tlsSkipVerify": true}`),
			SecureJSONData: json.RawMessage(`{"httpHeaderValue1": "Bearer ` + token + `"}`),
		},
		GrafanaCommonSpec: grafanav1beta1.GrafanaCommonSpec{
			InstanceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":      "grafana",
					"app.kubernetes.io/instance":  instance.Name,
					"app.kubernetes.io/component": "grafana",
				},
			},
		},
	}
	op, err := CreateOrUpdateWithRetries(ctx, r.Client, tempoDataSource, func() error {
		tempoDataSource.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "tempo")
		tempoDataSource.Spec = tempoDataSourceSpec
		return ctrl.SetControllerReference(instance, tempoDataSource, r.Scheme)
	})
	if err != nil {
		return err
	}
	if op == ctrlutil.OperationResultCreated {
		logger.Info("Created Tempo datasource")
	} else if op == ctrlutil.OperationResultUpdated {
		logger.Info("Updated Tempo datasource")
	}
	return nil
}

// helper function to extract the token value from json.RawMessage(`{"httpHeaderValue1": json.RawMessage(`{"httpHeaderValue1": "Bearer ` + token + `"}`),}`),
func extractTokenFromSecureJSONData(secureJSONData json.RawMessage) (string, error) {
	var data map[string]string
	err := json.Unmarshal(secureJSONData, &data)
	if err != nil {
		return "", err
	}
	// Extract the token value from secureJSONData and remove the "Bearer " prefix
	token, ok := data["httpHeaderValue1"]
	if !ok {
		return "", fmt.Errorf("token not found in secureJSONData")
	}

	// Remove "Bearer " prefix if present
	const bearerPrefix = "Bearer "
	if len(token) > len(bearerPrefix) && token[:len(bearerPrefix)] == bearerPrefix {
		token = token[len(bearerPrefix):]
	} else {
		return "", fmt.Errorf("token does not have 'Bearer ' prefix")
	}
	// Check if the token is empty after removing the prefix
	if token == "" {
		return "", fmt.Errorf("token is empty after removing 'Bearer ' prefix")
	}

	return token, nil
}

// DSProxyConfig represents the dsproxy configuration
type DSProxyConfig struct {
	Proxies []DSProxyRule `yaml:"proxies"`
}

// DSProxyRule defines a rule for redirecting traffic to a specific domain and ports
type DSProxyRule struct {
	Domain  string         `yaml:"domain"`
	Proxies []DSProxyPorts `yaml:"proxies"`
}

// DSProxyPorts defines the HTTP and HTTPS ports for proxying
type DSProxyPorts struct {
	HTTP  []int `yaml:"http,omitempty"`
	HTTPS []int `yaml:"https,omitempty"`
}

// parseURLHostPort parses a URL and extracts the hostname and port
// Returns hostname, port, and scheme (http/https)
func parseURLHostPort(urlStr string) (string, int, string, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", 0, "", fmt.Errorf("failed to parse URL %s: %w", urlStr, err)
	}

	hostname := parsedURL.Hostname()
	if hostname == "" {
		return "", 0, "", fmt.Errorf("empty hostname in URL %s", urlStr)
	}

	scheme := parsedURL.Scheme
	if scheme == "" {
		return "", 0, "", fmt.Errorf("URL must include scheme (http:// or https://): %s", urlStr)
	}

	// Validate scheme
	if scheme != "http" && scheme != "https" {
		return "", 0, "", fmt.Errorf("URL scheme must be http or https, got: %s", scheme)
	}

	// Get port, use default ports if not specified
	portStr := parsedURL.Port()
	var port int
	if portStr == "" {
		// Use default ports based on scheme
		switch scheme {
		case "https":
			port = 443
		case "http":
			port = 80
		}
	} else {
		port, err = strconv.Atoi(portStr)
		if err != nil {
			return "", 0, "", fmt.Errorf("invalid port in URL %s: %w", urlStr, err)
		}
	}

	return hostname, port, scheme, nil
}

// buildDSProxyConfig builds the dsproxy configuration from the datasources
func (r *GrafanaReconciler) buildDSProxyConfig(ctx context.Context, instance *grafoov1alpha1.Grafana) (*DSProxyConfig, error) {
	logger := log.FromContext(ctx)

	// Map to group ports by domain
	domainMap := make(map[string]map[string]map[int]bool) // domain -> scheme -> port -> exists

	// Iterate through all datasources and extract URLs
	for _, ds := range instance.Spec.DataSources {
		if !ds.Enabled {
			continue
		}

		var urlStr string
		switch ds.Type {
		case grafoov1alpha1.PrometheusInCluster, grafoov1alpha1.PrometheusMcoo:
			if ds.Prometheus != nil {
				urlStr = ds.Prometheus.URL
			}
		case grafoov1alpha1.LokiInCluster:
			if ds.Loki != nil {
				urlStr = ds.Loki.URL
			}
		case grafoov1alpha1.TempoInCluster:
			if ds.Tempo != nil {
				urlStr = ds.Tempo.URL
			}
		}

		if urlStr == "" {
			logger.Info("Skipping datasource with empty URL", "datasource", ds.Name)
			continue
		}

		hostname, port, scheme, err := parseURLHostPort(urlStr)
		if err != nil {
			logger.Error(err, "Failed to parse datasource URL", "datasource", ds.Name, "url", urlStr)
			continue
		}

		// Initialize maps if needed
		if domainMap[hostname] == nil {
			domainMap[hostname] = make(map[string]map[int]bool)
		}
		if domainMap[hostname][scheme] == nil {
			domainMap[hostname][scheme] = make(map[int]bool)
		}

		// Add port to the map
		domainMap[hostname][scheme][port] = true
		logger.Info("Added datasource to dsproxy config", "datasource", ds.Name, "hostname", hostname, "port", port, "scheme", scheme)
	}

	// Build the DSProxyConfig from the map
	config := &DSProxyConfig{
		Proxies: []DSProxyRule{},
	}

	for domain, schemes := range domainMap {
		rule := DSProxyRule{
			Domain:  domain,
			Proxies: []DSProxyPorts{},
		}

		ports := DSProxyPorts{}

		// Collect HTTP ports
		if httpPorts, ok := schemes["http"]; ok {
			for port := range httpPorts {
				ports.HTTP = append(ports.HTTP, port)
			}
		}

		// Collect HTTPS ports
		if httpsPorts, ok := schemes["https"]; ok {
			for port := range httpsPorts {
				ports.HTTPS = append(ports.HTTPS, port)
			}
		}

		// Only add the rule if there are ports
		if len(ports.HTTP) > 0 || len(ports.HTTPS) > 0 {
			rule.Proxies = append(rule.Proxies, ports)
			config.Proxies = append(config.Proxies, rule)
		}
	}

	return config, nil
}

// reconcileDSProxyConfig creates or updates the ConfigMap containing the dsproxy configuration
func (r *GrafanaReconciler) reconcileDSProxyConfig(ctx context.Context, instance *grafoov1alpha1.Grafana) error {
	logger := log.FromContext(ctx)

	// Build the dsproxy configuration
	config, err := r.buildDSProxyConfig(ctx, instance)
	if err != nil {
		return fmt.Errorf("failed to build dsproxy config: %w", err)
	}

	// Marshal to YAML
	configYAML, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal dsproxy config to YAML: %w", err)
	}

	// Create or update the ConfigMap
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, "dsproxy-config"),
			Namespace: instance.Namespace,
		},
	}

	op, err := CreateOrUpdateWithRetries(ctx, r.Client, configMap, func() error {
		configMap.ObjectMeta.Labels = r.generateLabelsForComponent(instance, "dsproxy")
		configMap.Data = map[string]string{
			"dsproxy.yaml": string(configYAML),
		}
		return ctrl.SetControllerReference(instance, configMap, r.Scheme)
	})

	if err != nil {
		return fmt.Errorf("failed to create or update dsproxy ConfigMap: %w", err)
	}

	if op == ctrlutil.OperationResultCreated {
		logger.Info("Created dsproxy ConfigMap")
	} else if op == ctrlutil.OperationResultUpdated {
		logger.Info("Updated dsproxy ConfigMap")
	}

	return nil
}
