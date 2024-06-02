package defaults

var (
	DexImage       = "docker.io/dexidp/dex:v2.39.1-distroless"
	GrafanaVersion = "9.5.17"
	DexHttpPort    = int32(5555)
	DexGrpcPort    = int32(5556)
	DexMetricsPort = int32(5557)
)
