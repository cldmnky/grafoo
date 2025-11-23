package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/coreos/go-iptables/iptables"
	"github.com/fsnotify/fsnotify"
	flag "github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Proxies []ProxyRule `yaml:"proxies"`
}

// ProxyRule defines a rule for redirecting traffic
// to a specific domain and ports.
// Example config.yaml:
//
// proxies:
//   - domain: example.com
//     proxies:
//     http: [80, 8080]
//     https: [443, 8443]
//   - domain: another.com
//     proxies:
//     http: [80]
//     https: [443]
//
// Save this as /etc/dsproxy/config.yaml or specify with --config flag.
type ProxyRule struct {
	Domain  string    `yaml:"domain"`
	Proxies []Proxies `yaml:"proxies"`
}

type Proxies struct {
	http  []int `yaml:"http"`
	https []int `yaml:"https"`
}

const (
	redirectPortHTTP  = 5533
	redirectPortHTTPS = 5534
	ipTableTarget     = "nat"
	ipTableChain      = "OUTPUT"
)

// Interface for iptables operations
type iptablesInterface interface {
	Exists(table, chain string, rulespec ...string) (bool, error)
	AppendUnique(table, chain string, rulespec ...string) error
}

var resolveDomainIP = func(domain string) (string, error) {
	ips, err := net.LookupHost(domain)
	if err != nil {
		return "", err
	}
	if len(ips) == 0 {
		return "", fmt.Errorf("no IPs found for %s", domain)
	}
	return ips[0], nil
}

func iptablesRuleExists(ipt iptablesInterface, ip string, port int) bool {
	exists, err := ipt.Exists(
		ipTableTarget,
		ipTableChain,
		"-p", "tcp",
		"-d", ip,
		"--dport", fmt.Sprintf("%d", port),
		"-j", "REDIRECT",
		"--to-port", fmt.Sprintf("%d", redirectPortHTTP),
	)
	if err != nil {
		log.Printf("Error checking for iptables rule %s:%d: %v", ip, port, err)
		return false // Assume rule doesn't exist if there's an error checking
	}
	return exists
}

func addIptablesRule(ipt iptablesInterface, ip string, port int) error {
	if iptablesRuleExists(ipt, ip, port) {
		log.Printf("iptables rule already exists for %s:%d", ip, port)
		return nil
	}
	ruleSpec := []string{
		"-p", "tcp",
		"-d", ip,
		"--dport", fmt.Sprintf("%d", port),
		"-j", "REDIRECT",
		"--to-port", fmt.Sprintf("%d", redirectPortHTTP),
	}
	log.Printf("Adding iptables rule: %s", strings.Join(ruleSpec, " "))
	return ipt.AppendUnique(ipTableTarget, ipTableChain, ruleSpec...)
}

func loadConfig() (*Config, error) {
	data, err := os.ReadFile(f_configPath)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func applyRules(ipt iptablesInterface, cfg *Config) {
	for _, rule := range cfg.Proxies {
		ip, err := resolveDomainIP(rule.Domain)
		if err != nil {
			log.Printf("DNS lookup failed for %s: %v", rule.Domain, err)
			continue
		}
		for _, proxies := range rule.Proxies {
			for _, p := range proxies.http {
				if err := addIptablesRule(ipt, ip, p); err != nil {
					log.Printf("Failed to add iptables rule for %s:%d: %v", ip, p, err)
				}
			}
			for _, p := range proxies.https {
				if err := addIptablesRule(ipt, ip, p); err != nil {
					log.Printf("Failed to add iptables rule for %s:%d: %v", ip, p, err)
				}
			}
		}
	}
}

func watchConfig(ctx context.Context, path string, onChange func()) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatalf("Failed to watch config: %v", err)
	}
	defer watcher.Close()

	if err := watcher.Add(path); err != nil {
		log.Fatalf("Could not watch config file: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("Stopping config watcher")
			return
		case event := <-watcher.Events:
			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				log.Println("Config file changed — reloading...")
				onChange()
			}
		case err := <-watcher.Errors:
			log.Println("Watcher error:", err)
		}
	}
}

// Flags
var (
	f_iptables       bool
	f_configPath     string
	f_tlsCert        string
	f_tlsKey         string
	f_jwksURL        string
	f_policyPath     string
	f_upstreamURL    string
	f_injectionLabel string
	f_jwtAudience    string
	f_caBundle       string
)

func init() {
	flag.StringVar(&f_configPath, "config",
		getenvOrDefault("DSPROXY_CONFIG", "/etc/dsproxy/config/dsproxy.yaml"),
		"Path to config file")

	flag.BoolVar(&f_iptables, "iptables",
		getenvBoolOrDefault("DSPROXY_IPTABLES", true),
		"Enable iptables support")

	flag.StringVar(&f_tlsCert, "tls-cert",
		getenvOrDefault("DSPROXY_TLS_CERT", "/etc/dsproxy/tls/tls.crt"),
		"Path to TLS certificate file")

	flag.StringVar(&f_tlsKey, "tls-key",
		getenvOrDefault("DSPROXY_TLS_KEY", "/etc/dsproxy/tls/tls.key"),
		"Path to TLS key file")

	flag.StringVar(&f_jwksURL, "jwks-url",
		getenvOrDefault("DSPROXY_JWKS_URL", "https://oidc/.well-known/openid-configuration"),
		"OIDC discovery URL")

	flag.StringVar(&f_policyPath, "policy-path",
		getenvOrDefault("DSPROXY_POLICY_PATH", "/etc/dsproxy/policy"),
		"Path to policy directory")

	flag.StringVar(&f_upstreamURL, "upstream-url",
		getenvOrDefault("DSPROXY_UPSTREAM_URL", "http://localhost:9090"),
		"Upstream Prometheus URL")

	flag.StringVar(&f_injectionLabel, "injection-label",
		getenvOrDefault("DSPROXY_INJECTION_LABEL", "namespace"),
		"Label name to inject for multi-tenancy (e.g., namespace, tenant)")

	flag.StringVar(&f_jwtAudience, "jwt-audience",
		getenvOrDefault("DSPROXY_JWT_AUDIENCE", "example-app"),
		"Expected JWT audience claim")

	flag.StringVar(&f_caBundle, "ca-bundle",
		getenvOrDefault("DSPROXY_CA_BUNDLE", ""),
		"Path to CA bundle file for verifying upstream and JWKS certificates")
}

func getenvOrDefault(envVar, fallback string) string {
	if val := os.Getenv(envVar); val != "" {
		return val
	}
	return fallback
}

func getenvBoolOrDefault(envVar string, fallback bool) bool {
	if val := os.Getenv(envVar); val != "" {
		return val == "1" || val == "true" || val == "TRUE"
	}
	return fallback
}

func startServers(authzService *AuthzService, promProxy http.Handler) (httpServer, httpsServer *http.Server) {
	mux := http.NewServeMux()

	// Middleware chain: auth -> authz -> prom-label-proxy
	// 1. authMiddleware: validates JWT and extracts sub/groups
	// 2. authzMiddleware: checks policy.csv and populates allowed cluster/namespace pairs
	// 3. prom-label-proxy: injects namespace labels based on authorized resources
	handler := authMiddleware(
		authzService.authzMiddleware("read")(
			promProxy,
		),
	)

	mux.Handle("/", handler)

	httpServer = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", redirectPortHTTP),
		Handler: mux,
	}
	log.Println("Starting HTTP server on port", redirectPortHTTP)

	go func() {
		if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server error: %v", err)
		}
		log.Println("Stopped serving new HTTP connections.")
	}()

	if f_tlsCert != "" && f_tlsKey != "" {
		httpsMux := http.NewServeMux()
		httpsHandler := authMiddleware(
			authzService.authzMiddleware("read")(
				promProxy,
			),
		)
		httpsMux.Handle("/", httpsHandler)
		httpsServer = &http.Server{
			Addr:    fmt.Sprintf("127.0.0.1:%d", redirectPortHTTPS),
			Handler: httpsMux,
		}
		log.Println("Starting HTTPS server on port", redirectPortHTTPS)
		go func() {
			if err := httpsServer.ListenAndServeTLS(f_tlsCert, f_tlsKey); !errors.Is(err, http.ErrServerClosed) {
				log.Fatalf("HTTPS server error: %v", err)
			}
			log.Println("Stopped serving new HTTPS connections.")
		}()
	}
	return httpServer, httpsServer
}

func main() {
	flag.Parse()
	if f_iptables {
		log.Println("Iptables support enabled")
	}
	if f_tlsCert != "" && f_tlsKey != "" {
		log.Println("TLS support enabled")
	} else {
		log.Println("TLS support disabled")
	}
	log.Println("Config file path:", f_configPath)
	log.Println("TLS certificate path:", f_tlsCert)
	log.Println("TLS key path:", f_tlsKey)
	log.Println("JWKS URL:", f_jwksURL)
	log.Println("Policy path:", f_policyPath)
	log.Println("Redirect port HTTP:", redirectPortHTTP)
	log.Println("Redirect port HTTPS:", redirectPortHTTPS)
	log.Println("Starting dsproxy...")

	// Create a context that is canceled on signal
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var ipt iptablesInterface
	var err error

	if f_iptables {
		if os.Geteuid() != 0 {
			log.Fatal("Run as root for iptables support")
		}

		if f_configPath == "" {
			log.Fatal("Config file path is required")
		}
		if _, err := os.Stat(f_configPath); os.IsNotExist(err) {
			log.Fatalf("Config file does not exist: %s", f_configPath)
		}
		ipt, err = iptables.New()
		if err != nil {
			log.Fatalf("Failed to initialize iptables: %v", err)
		}

		cfg, err := loadConfig()
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
		applyRules(ipt, cfg)
	}

	if f_iptables {
		// Watch for config changes
		go watchConfig(ctx, f_configPath, func() {
			newCfg, err := loadConfig()
			if err != nil {
				log.Printf("Reload failed: %v", err)
				return
			}
			applyRules(ipt, newCfg)
		})
	}

	authzService, err := NewAuthzService(ctx, f_policyPath)
	if err != nil {
		log.Fatalf("Failed to initialize authorization service: %v", err)
	}

	// Initialize JWKS before serving requests
	if err := initJWKS(); err != nil {
		log.Fatalf("Failed to initialize JWKS: %v", err)
	}

	// Create Prometheus proxy with label injection
	promProxy, err := newPrometheusProxy(f_upstreamURL, f_injectionLabel)
	if err != nil {
		log.Fatalf("Failed to create Prometheus proxy: %v", err)
	}
	log.Printf("Prometheus proxy created: upstream=%s, label=%s", f_upstreamURL, f_injectionLabel)

	httpServer, httpsServer := startServers(authzService, promProxy)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	log.Println("Received shutdown signal")
	cancel() // Cancel context to stop watchers

	shutdownCtx, shutdownRelease := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownRelease()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("HTTP shutdown error: %v", err)
	}
	if httpsServer != nil {
		if err := httpsServer.Shutdown(shutdownCtx); err != nil {
			log.Fatalf("HTTPS shutdown error: %v", err)
		}
	}
	log.Println("Graceful shutdown complete.")
}
