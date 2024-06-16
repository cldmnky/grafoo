package v1alpha1

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// Phases
	PhasePending   = "Pending"
	PhaseRunning   = "Running"
	PhaseSucceeded = "Succeeded"
	PhaseFailed    = "Failed"
)

var (
	DexImage           = "docker.io/dexidp/dex:v2.39.1-distroless"
	GrafanaVersion     = "9.5.17"
	TokenDuration      = metav1.Duration{Duration: 1440 * time.Minute}
	GrafanaReplicas    = int32(2)
	DexHttpPort        = int32(5555)
	DexGrpcPort        = int32(5556)
	DexMetricsPort     = int32(5557)
	MariaDBStorageSize = "5Gi"
	MariaDBImage       = "registry.access.redhat.com/rhel9/mariadb-1011:1-12"
	DataSources        = []DataSource{
		{
			Name:    "Prometheus",
			Type:    "prometheus-incluster",
			Enabled: true,
			Prometheus: &PrometheusDS{
				URL: "https://thanos-querier.openshift-monitoring.svc.cluster.local:9091",
			},
		},
		{
			Name:    "Loki (Application)",
			Type:    "loki-incluster",
			Enabled: true,
			Loki: &LokiDS{
				URL: "https://logging-loki-gateway-http.openshift-logging.svc.cluster.local:8080/api/logs/v1/application/",
			},
		},
		{
			Name:    "Loki (Infrastructure)",
			Type:    "loki-incluster",
			Enabled: true,
			Loki: &LokiDS{
				URL: "https://logging-loki-gateway-http.openshift-logging.svc.cluster.local:8080/api/logs/v1/infrastructure/",
			},
		},
		{
			Name:    "Loki (Audit)",
			Type:    "loki-incluster",
			Enabled: true,
			Loki: &LokiDS{
				URL: "https://logging-loki-gateway-http.openshift-logging.svc.cluster.local:8080/api/logs/v1/audit/",
			},
		},
		{
			Name:    "Tempo (Dev)",
			Type:    "tempo-incluster",
			Enabled: true,
			Tempo: &TempoDS{
				URL: "https://tempo-tempo-gateway.openshift-tempo-operator.svc.cluster.local:8080/api/traces/v1/dev/tempo",
			},
		},
		{
			Name:    "Tempo (Prod)",
			Type:    "tempo-incluster",
			Enabled: true,
			Tempo: &TempoDS{
				URL: "https://tempo-tempo-gateway.openshift-tempo-operator.svc.cluster.local:8080/api/traces/v1/prod/tempo",
			},
		},
	}
)
