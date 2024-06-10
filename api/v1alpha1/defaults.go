package v1alpha1

var (
	DexImage        = "docker.io/dexidp/dex:v2.39.1-distroless"
	GrafanaVersion  = "9.5.17"
	GrafanaReplicas = int32(1)
	DexHttpPort     = int32(5555)
	DexGrpcPort     = int32(5556)
	DexMetricsPort  = int32(5557)
	DataSources     = []DataSource{
		{
			Name:    "Prometheus",
			Type:    "prometheus-incluster",
			URL:     "https://thanos-querier.openshift-monitoring.svc.cluster.local:9091",
			Enabled: true,
		},
		{
			Name:    "Loki (Application)",
			Type:    "loki-incluster",
			URL:     "https://logging-loki-gateway-http.openshift-logging.svc.cluster.local:8080/api/logs/v1/application/",
			Enabled: true,
		},
		{
			Name:    "Loki (Infrastructure)",
			Type:    "loki-incluster",
			URL:     "https://logging-loki-gateway-http.openshift-logging.svc.cluster.local:8080/api/logs/v1/infrastructure/",
			Enabled: true,
		},
		{
			Name:    "Loki (Audit)",
			Type:    "loki-incluster",
			URL:     "https://logging-loki-gateway-http.openshift-logging.svc.cluster.local:8080/api/logs/v1/audit/",
			Enabled: true,
		},
	}
)
