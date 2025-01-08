package controller

import (
	"context"
	"encoding/json"

	grafanav1beta1 "github.com/grafana/grafana-operator/v5/api/v1beta1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
)

func (r *GrafanaReconciler) ReconcileDataSources(ctx context.Context, instance *grafoov1alpha1.Grafana) error {
	logger := log.FromContext(ctx)
	// Create a datasource for the Grafana instance for prometheus
	request := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			Audiences:         nil,
			ExpirationSeconds: int64Ptr(int64(instance.Spec.TokenDuration.Duration.Seconds())),
		},
	}
	resp, err := r.Clientset.CoreV1().ServiceAccounts(instance.Namespace).CreateToken(ctx, r.generateNameForComponent(instance, "sa"), request, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	logger.Info("Created token for datasources", "token expiration", resp.Status.ExpirationTimestamp.Time)
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
	// create a labels.Selector for the datasources
	// "app.kubernetes.io/instance":  instance.Name,
	dsSelector := labels.SelectorFromSet(labels.Set{
		"app.kubernetes.io/instance": instance.Name,
	})
	err = r.Client.List(ctx, datasources, &client.ListOptions{Namespace: instance.Namespace, LabelSelector: dsSelector})
	if err != nil {
		return err
	}

	var reconciledDatasources = make(map[string]bool)
	for _, ds := range instance.Spec.DataSources {
		switch ds.Type {
		case "prometheus-incluster":
			err = r.reconcilePrometheusDataSource(ctx, instance, ds, resp)
			if err != nil {
				return err
			}
			// add the datasource to the list of reconciled datasources
			reconciledDatasources[r.generateNameForComponent(instance, ds.GetDataSourceNameHash())] = true
		case "loki-incluster":
			err = r.reconcileLokiDataSource(ctx, instance, ds, resp)
			if err != nil {
				return err
			}
			// add the datasource to the list of reconciled datasources
			reconciledDatasources[r.generateNameForComponent(instance, ds.GetDataSourceNameHash())] = true
		case "tempo-incluster":
			err = r.reconcileTempoDataSource(ctx, instance, ds, resp)
			if err != nil {
				return err
			}
			// add the datasource to the list of reconciled datasources
			reconciledDatasources[r.generateNameForComponent(instance, ds.GetDataSourceNameHash())] = true
		default:
			logger.Info("Unknown datasource type", "type", ds.Type)
		}
	}
	for _, ds := range datasources.Items {
		if _, ok := reconciledDatasources[ds.Name]; !ok {
			logger.Info("Deleting datasource", "name", ds.Name)
			err = r.Client.Delete(ctx, &ds)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *GrafanaReconciler) reconcilePrometheusDataSource(ctx context.Context, instance *grafoov1alpha1.Grafana, ds grafoov1alpha1.DataSource, resp *authenticationv1.TokenRequest) error {
	logger := log.FromContext(ctx)
	promDataSource := &grafanav1beta1.GrafanaDatasource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, ds.GetDataSourceNameHash()),
			Namespace: instance.Namespace,
			Labels:    r.generateLabelsForComponent(instance, "grafana"),
		},
	}
	promDataSourceSpec := grafanav1beta1.GrafanaDatasourceSpec{
		Datasource: &grafanav1beta1.GrafanaDatasourceInternal{
			Name:           ds.Name,
			Type:           "prometheus",
			Access:         "proxy",
			IsDefault:      boolPtr(true),
			URL:            ds.Prometheus.URL,
			JSONData:       json.RawMessage(`{"httpHeaderName1": "Authorization", "tlsSkipVerify": true}`),
			SecureJSONData: json.RawMessage(`{"httpHeaderValue1": "Bearer ` + resp.Status.Token + `"}`),
		},
		InstanceSelector: &metav1.LabelSelector{
			MatchLabels: r.generateLabelsForComponent(instance, "grafana"),
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

func (r *GrafanaReconciler) reconcileLokiDataSource(ctx context.Context, instance *grafoov1alpha1.Grafana, ds grafoov1alpha1.DataSource, resp *authenticationv1.TokenRequest) error {
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
			SecureJSONData: json.RawMessage(`{"httpHeaderValue1": "Bearer ` + resp.Status.Token + `"}`),
		},
		InstanceSelector: &metav1.LabelSelector{
			MatchLabels: r.generateLabelsForComponent(instance, "grafana"),
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
func (r *GrafanaReconciler) reconcileTempoDataSource(ctx context.Context, instance *grafoov1alpha1.Grafana, ds grafoov1alpha1.DataSource, resp *authenticationv1.TokenRequest) error {
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
			SecureJSONData: json.RawMessage(`{"httpHeaderValue1": "Bearer ` + resp.Status.Token + `"}`),
		},
		InstanceSelector: &metav1.LabelSelector{
			MatchLabels: r.generateLabelsForComponent(instance, "grafana"),
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
