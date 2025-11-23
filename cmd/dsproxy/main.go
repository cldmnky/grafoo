package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	flag "github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
)

type Config struct {
	Proxies []ProxyRule `yaml:"Proxies"`
}

// ProxyRule defines a rule for redirecting traffic
// to a specific domain and ports.
// Example config.yaml:
//
// Proxies:
//   - Domain: example.com
//     Proxies:
//     HTTP: [80, 8080]
//     HTTPS: [443, 8443]
//   - Domain: another.com
//     Proxies:
//     HTTP: [80]
//     HTTPS: [443]
//
// Save this as /etc/dsproxy/config.yaml or specify with --config flag.
type ProxyRule struct {
	Domain  string    `yaml:"Domain"`
	Proxies []Proxies `yaml:"Proxies"`
}

type Proxies struct {
	HTTP  []int `yaml:"HTTP"`
	HTTPS []int `yaml:"HTTPS"`
}

const (
	redirectPortHTTP  = 5533
	redirectPortHTTPS = 5534
)

// Interface for nftables operations
type nftablesInterface interface {
	AddRedirectRule(ip string, port, targetPort int) error
	FlushRules() error
}

type nftablesRunner struct{}

func (r *nftablesRunner) FlushRules() error {
	cmd := exec.Command("nft", "flush", "ruleset")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to flush ruleset: %s: %w", string(out), err)
	}

	// Initialize basic table and chain
	initCmds := []string{
		"add table ip nat",
		"add chain ip nat OUTPUT { type nat hook output priority 0; policy accept; }",
	}

	for _, c := range initCmds {
		args := strings.Split(c, " ")
		cmd := exec.Command("nft", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to run nft command '%s': %s: %w", c, string(out), err)
		}
	}
	return nil
}

func (r *nftablesRunner) AddRedirectRule(ip string, port, targetPort int) error {
	// nft add rule ip nat OUTPUT ip daddr 1.2.3.4 tcp dport 80 counter dnat to 127.0.0.1:5533
	args := []string{
		"add", "rule", "ip", "nat", "OUTPUT",
		"ip", "daddr", ip,
		"tcp", "dport", fmt.Sprintf("%d", port),
		"counter",
		"dnat", "to", fmt.Sprintf("127.0.0.1:%d", targetPort),
	}

	cmd := exec.Command("nft", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add rule: %s: %w", string(out), err)
	}
	log.Printf("Added nftables rule for %s:%d -> 127.0.0.1:%d", ip, port, targetPort)
	return nil
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

func applyRules(nft nftablesInterface, cfg *Config) {
	// Flush existing rules first to avoid duplicates
	if err := nft.FlushRules(); err != nil {
		log.Printf("Failed to flush nftables rules: %v", err)
	}

	for _, rule := range cfg.Proxies {
		ip, err := resolveDomainIP(rule.Domain)
		if err != nil {
			log.Printf("DNS lookup failed for %s: %v", rule.Domain, err)
			continue
		}
		for _, proxies := range rule.Proxies {
			for _, p := range proxies.HTTP {
				if err := nft.AddRedirectRule(ip, p, redirectPortHTTP); err != nil {
					log.Printf("Failed to add nftables rule for %s:%d: %v", ip, p, err)
				}
			}
			for _, p := range proxies.HTTPS {
				if err := nft.AddRedirectRule(ip, p, redirectPortHTTPS); err != nil {
					log.Printf("Failed to add nftables rule for %s:%d: %v", ip, p, err)
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
	f_injectionLabel string
	f_jwtAudience    string
	f_caBundle       string
	f_tokenReview    bool
)

func init() {
	flag.StringVar(&f_configPath, "config",
		getenvOrDefault("DSPROXY_CONFIG", "/etc/dsproxy/config/dsproxy.yaml"),
		"Path to config file")

	flag.BoolVar(&f_iptables, "iptables",
		getenvBoolOrDefault("DSPROXY_IPTABLES", true),
		"Enable iptables support")

	flag.BoolVar(&f_tokenReview, "token-review",
		getenvBoolOrDefault("DSPROXY_TOKEN_REVIEW", false),
		"Enable Kubernetes TokenReview for validation")

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

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("Started %s %s", r.Method, r.URL.Path)

		// Debug: Dump request headers
		reqDump, err := httputil.DumpRequest(r, true)
		if err != nil {
			log.Printf("Failed to dump request: %v", err)
		} else {
			log.Printf("Request Dump:\n%s", string(reqDump))
		}

		next.ServeHTTP(w, r)
		log.Printf("Completed %s %s in %v", r.Method, r.URL.Path, time.Since(start))
	})
}

func startServers(authzService *AuthzService, httpProxy, httpsProxy http.Handler, k8sClient client.Client) (httpServer, httpsServer *http.Server) {
	// API Handler
	apiMux := http.NewServeMux()
	if k8sClient != nil {
		apiHandler := NewAPIHandler(k8sClient)
		apiHandler.RegisterRoutes(apiMux)
	}
	apiHandler := authMiddleware(apiMux)

	// UI Handler
	ui := uiHandler()

	// Proxy Handler
	// Middleware chain: auth -> authz -> prom-label-proxy
	proxyHandler := authMiddleware(
		authzService.authzMiddleware("read")(
			httpProxy,
		),
	)

	// Main Handler with dispatch logic
	mainHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Management API
		if strings.HasPrefix(r.URL.Path, "/api/v1/rules") {
			apiHandler.ServeHTTP(w, r)
			return
		}

		// UI
		if strings.HasPrefix(r.URL.Path, "/ui/") || r.URL.Path == "/ui" {
			http.StripPrefix("/ui", ui).ServeHTTP(w, r)
			return
		}

		// Default: Proxy
		proxyHandler.ServeHTTP(w, r)
	})

	httpServer = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", redirectPortHTTP),
		Handler: loggingMiddleware(mainHandler),
	}
	log.Println("Starting HTTP server on port", redirectPortHTTP)

	go func() {
		if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server error: %v", err)
		}
		log.Println("Stopped serving new HTTP connections.")
	}()

	if f_tlsCert != "" && f_tlsKey != "" {
		httpsProxyHandler := authMiddleware(
			authzService.authzMiddleware("read")(
				httpsProxy,
			),
		)

		httpsMainHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/v1/rules") {
				apiHandler.ServeHTTP(w, r)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/ui/") || r.URL.Path == "/ui" {
				http.StripPrefix("/ui", ui).ServeHTTP(w, r)
				return
			}
			httpsProxyHandler.ServeHTTP(w, r)
		})

		httpsServer = &http.Server{
			Addr:    fmt.Sprintf("127.0.0.1:%d", redirectPortHTTPS),
			Handler: loggingMiddleware(httpsMainHandler),
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

	var nft nftablesInterface

	if f_iptables {
		if os.Geteuid() != 0 {
			log.Fatal("Run as root for nftables support")
		}

		if f_configPath == "" {
			log.Fatal("Config file path is required")
		}
		if _, err := os.Stat(f_configPath); os.IsNotExist(err) {
			log.Fatalf("Config file does not exist: %s", f_configPath)
		}

		nft = &nftablesRunner{}

		cfg, err := loadConfig()
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
		applyRules(nft, cfg)
	}

	if f_iptables {
		// Watch for config changes
		go watchConfig(ctx, f_configPath, func() {
			newCfg, err := loadConfig()
			if err != nil {
				log.Printf("Reload failed: %v", err)
				return
			}
			applyRules(nft, newCfg)
		})
	}

	// Initialize Kubernetes client
	var k8sClient client.Client
	k8sConfig, err := ctrl.GetConfig()
	if err != nil {
		log.Printf("Failed to get k8s config (running in file-only mode): %v", err)
	} else {
		if err := grafoov1alpha1.AddToScheme(scheme.Scheme); err != nil {
			log.Fatalf("Failed to add grafoo types to scheme: %v", err)
		}
		k8sClient, err = client.New(k8sConfig, client.Options{Scheme: scheme.Scheme})
		if err != nil {
			log.Fatalf("Failed to create k8s client: %v", err)
		}
		log.Println("Kubernetes client initialized")
	}

	authzService, err := NewAuthzService(ctx, f_policyPath, k8sClient)
	if err != nil {
		log.Fatalf("Failed to initialize authorization service: %v", err)
	}

	// Initialize Auth (JWKS or TokenReview)
	if err := initAuth(); err != nil {
		log.Fatalf("Failed to initialize auth: %v", err)
	}

	// Create Dynamic Proxies for HTTP and HTTPS
	httpProxy := NewDynamicProxy("http", f_injectionLabel)
	httpsProxy := NewDynamicProxy("https", f_injectionLabel)

	log.Printf("Dynamic proxies created with label=%s", f_injectionLabel)

	httpServer, httpsServer := startServers(authzService, httpProxy, httpsProxy, k8sClient)

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
