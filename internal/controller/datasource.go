package controller

import (
	"context"
	"encoding/json"

	grafanav1beta1 "github.com/grafana/grafana-operator/v5/api/v1beta1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
)

func (r *GrafanaReconciler) ReconcileDataSource(ctx context.Context, instance *grafoov1alpha1.Grafana) error {
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

	for _, ds := range instance.Spec.DataSources {
		switch ds.Type {
		case "prometheus-incluster":
			err = r.reconcilePrometheusDataSource(ctx, instance, ds, resp)
			if err != nil {
				return err
			}
		case "loki-incluster":
			err = r.reconcileLokiDataSource(ctx, instance, ds, resp)
			if err != nil {
				return err
			}
		case "tempo-incluster":
			//err = r.reconcileTempoDataSource(ctx, instance)
			//if err != nil {
			//	return err
			//}
		default:
			logger.Info("Unknown datasource type", "type", ds.Type)
		}
	}
	/*
		// Loki datasource
		lokiDataSource := &grafanav1beta1.GrafanaDatasource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      r.generateNameForComponent(instance, "loki"),
				Namespace: instance.Namespace,
			},
		}
		lokiDataSourceSpec := grafanav1beta1.GrafanaDatasourceSpec{
			Datasource: &grafanav1beta1.GrafanaDatasourceInternal{
				Name:           "Loki Application",
				Type:           "loki",
				Access:         "proxy",
				IsDefault:      boolPtr(false),
				URL:            "https://loki.openshift-logging.svc.cluster.local:9095",
			},
		}
	*/

	return nil
}

func (r *GrafanaReconciler) reconcilePrometheusDataSource(ctx context.Context, instance *grafoov1alpha1.Grafana, ds grafoov1alpha1.DataSource, resp *authenticationv1.TokenRequest) error {
	logger := log.FromContext(ctx)
	promDataSource := &grafanav1beta1.GrafanaDatasource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForComponent(instance, ds.GetDataSourceNameHash()),
			Namespace: instance.Namespace,
		},
	}
	promDataSourceSpec := grafanav1beta1.GrafanaDatasourceSpec{
		Datasource: &grafanav1beta1.GrafanaDatasourceInternal{
			Name:           ds.Name,
			Type:           "prometheus",
			Access:         "proxy",
			IsDefault:      boolPtr(true),
			URL:            ds.URL,
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
		},
	}
	lokiDataSourceSpec := grafanav1beta1.GrafanaDatasourceSpec{
		Datasource: &grafanav1beta1.GrafanaDatasourceInternal{
			Name:           ds.Name,
			Type:           "loki",
			Access:         "proxy",
			IsDefault:      boolPtr(false),
			URL:            ds.URL,
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
