package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
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
	Domain string     `yaml:"domain"`
	Proxy  ProxyPorts `yaml:"proxies"`
}

// ProxyPorts defines which destination ports to redirect for a proxy rule.
// HTTP traffic is redirected to the plain HTTP listener (5533), HTTPS traffic
// to the TLS listener (5534).
type ProxyPorts struct {
	HTTP  []int `yaml:"http"`
	HTTPS []int `yaml:"https"`
}

const (
	redirectPortHTTP  = 5533
	redirectPortHTTPS = 5534
	ipTableTarget     = "nat"
	ipTableChain      = "OUTPUT"
	// dnsRefreshInterval is how often configured domains are re-resolved so
	// that iptables rules track DNS changes.
	dnsRefreshInterval = 60 * time.Second
)

// Interface for iptables operations
type iptablesInterface interface {
	Exists(table, chain string, rulespec ...string) (bool, error)
	AppendUnique(table, chain string, rulespec ...string) error
	Delete(table, chain string, rulespec ...string) error
}

// ruleSpec returns the iptables rulespec that redirects TCP traffic destined
// for ip:port to toPort on the local host.
func ruleSpec(ip string, port, toPort int) []string {
	return []string{
		"-p", "tcp",
		"-d", ip,
		"--dport", fmt.Sprintf("%d", port),
		"-j", "REDIRECT",
		"--to-port", fmt.Sprintf("%d", toPort),
	}
}

func validatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port %d out of range", port)
	}
	return nil
}

// iptablesManager tracks the rules DSProxy has installed so it can remove
// stale rules on reload and clean up on shutdown.
type iptablesManager struct {
	ipt         iptablesInterface
	installed   map[string]bool
	skipDomains map[string]bool
}

func newIptablesManager(ipt iptablesInterface) *iptablesManager {
	return &iptablesManager{
		ipt:         ipt,
		installed:   map[string]bool{},
		skipDomains: map[string]bool{},
	}
}

// addSkipDomain excludes a domain from interception. This is used for the
// upstream hostname so that DSProxy's own connections to the upstream are not
// redirected back into itself (self-interception loop).
func (m *iptablesManager) addSkipDomain(domain string) {
	m.skipDomains[domain] = true
	log.Printf("Skipping interception rules for upstream domain %s", domain)
}

// desiredRules resolves the configured rules to the set of iptables rules
// that should be installed, keyed by ip|port|toPort. DNS failures are logged
// and skipped so that remaining rules are still applied.
func (m *iptablesManager) desiredRules(cfg *Config) map[string]bool {
	desired := map[string]bool{}
	if cfg == nil {
		return desired
	}
	for _, rule := range cfg.Proxies {
		if rule.Domain == "" {
			log.Printf("Skipping proxy rule with empty domain")
			continue
		}
		if m.skipDomains[rule.Domain] {
			log.Printf("Skipping proxy rule for upstream domain %s", rule.Domain)
			continue
		}
		ip, err := resolveDomainIP(rule.Domain)
		if err != nil {
			log.Printf("DNS lookup failed for %s: %v", rule.Domain, err)
			continue
		}
		for _, port := range rule.Proxy.HTTP {
			if err := validatePort(port); err != nil {
				log.Printf("Skipping invalid HTTP port %d for %s: %v", port, rule.Domain, err)
				continue
			}
			desired[fmt.Sprintf("%s|%d|%d", ip, port, redirectPortHTTP)] = true
		}
		for _, port := range rule.Proxy.HTTPS {
			if err := validatePort(port); err != nil {
				log.Printf("Skipping invalid HTTPS port %d for %s: %v", port, rule.Domain, err)
				continue
			}
			desired[fmt.Sprintf("%s|%d|%d", ip, port, redirectPortHTTPS)] = true
		}
	}
	return desired
}

// apply makes the installed iptables rules match the desired configuration.
// Rules that are no longer desired (removed entries, changed DNS addresses)
// are deleted. It is idempotent and safe to call repeatedly.
func (m *iptablesManager) apply(cfg *Config) {
	desired := m.desiredRules(cfg)

	for key := range m.installed {
		if desired[key] {
			continue
		}
		parts := strings.Split(key, "|")
		if len(parts) != 3 {
			delete(m.installed, key)
			continue
		}
		port, err := strconv.Atoi(parts[1])
		if err != nil {
			delete(m.installed, key)
			continue
		}
		toPort, err := strconv.Atoi(parts[2])
		if err != nil {
			delete(m.installed, key)
			continue
		}
		spec := ruleSpec(parts[0], port, toPort)
		log.Printf("Removing iptables rule: %s", strings.Join(spec, " "))
		if err := m.ipt.Delete(ipTableTarget, ipTableChain, spec...); err != nil {
			log.Printf("Failed to delete iptables rule for %s: %v", parts[0], err)
			continue
		}
		delete(m.installed, key)
	}

	for key := range desired {
		if m.installed[key] {
			continue
		}
		parts := strings.Split(key, "|")
		if len(parts) != 3 {
			continue
		}
		port, _ := strconv.Atoi(parts[1])
		toPort, _ := strconv.Atoi(parts[2])
		spec := ruleSpec(parts[0], port, toPort)
		log.Printf("Adding iptables rule: %s", strings.Join(spec, " "))
		if err := m.ipt.AppendUnique(ipTableTarget, ipTableChain, spec...); err != nil {
			log.Printf("Failed to add iptables rule for %s:%d: %v", parts[0], port, err)
			continue
		}
		m.installed[key] = true
	}
}

// cleanup removes all iptables rules installed by this manager. It is called
// on shutdown so DSProxy does not leave interception rules behind.
func (m *iptablesManager) cleanup() {
	for key := range m.installed {
		parts := strings.Split(key, "|")
		if len(parts) != 3 {
			delete(m.installed, key)
			continue
		}
		port, _ := strconv.Atoi(parts[1])
		toPort, _ := strconv.Atoi(parts[2])
		spec := ruleSpec(parts[0], port, toPort)
		log.Printf("Cleaning up iptables rule: %s", strings.Join(spec, " "))
		if err := m.ipt.Delete(ipTableTarget, ipTableChain, spec...); err != nil {
			log.Printf("Failed to delete iptables rule for %s: %v", parts[0], err)
		}
		delete(m.installed, key)
	}
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
	if len(cfg.Proxies) == 0 {
		log.Printf("Warning: config %s defines no proxy rules", f_configPath)
	}
	return &cfg, nil
}

// watchConfig watches the directory containing the config file so that both
// in-place writes and atomic replacements (rename) are detected. Changes are
// debounced to avoid reloading multiple times per editor save.
func watchConfig(ctx context.Context, path string, onChange func()) {
	dir := filepath.Dir(path)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatalf("Failed to watch config: %v", err)
	}
	defer watcher.Close()

	if err := watcher.Add(dir); err != nil {
		log.Fatalf("Could not watch config directory %s: %v", dir, err)
	}
	log.Printf("Watching config directory %s", dir)

	var timer *time.Timer
	for {
		select {
		case <-ctx.Done():
			log.Println("Stopping config watcher")
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Name == path && event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) != 0 {
				log.Println("Config file changed - scheduling reload...")
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(100*time.Millisecond, func() {
					log.Println("Config file changed - reloading...")
					onChange()
				})
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Println("Watcher error:", err)
		}
	}
}

// watchDNS periodically re-resolves configured domains and reapplies the
// iptables rules so rules track DNS changes without a config file update.
func watchDNS(ctx context.Context, mgr *iptablesManager) {
	ticker := time.NewTicker(dnsRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cfg, err := loadConfig()
			if err != nil {
				log.Printf("Reload failed during DNS refresh: %v", err)
				continue
			}
			mgr.apply(cfg)
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
	f_jwtIssuer      string
	f_policyPath     string
	f_upstreamURL    string
	f_injectionLabel string
	f_clusterLabel   string
	f_jwtAudience    string
	f_caBundle       string
	f_uiPort         int
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

	flag.StringVar(&f_jwtIssuer, "jwt-issuer",
		getenvOrDefault("DSPROXY_JWT_ISSUER", ""),
		"Expected JWT issuer claim (empty to skip issuer validation)")

	flag.StringVar(&f_policyPath, "policy-path",
		getenvOrDefault("DSPROXY_POLICY_PATH", "/etc/dsproxy/policy"),
		"Path to policy directory")

	flag.StringVar(&f_upstreamURL, "upstream-url",
		getenvOrDefault("DSPROXY_UPSTREAM_URL", "http://localhost:9090"),
		"Upstream Prometheus URL")

	flag.StringVar(&f_injectionLabel, "injection-label",
		getenvOrDefault("DSPROXY_INJECTION_LABEL", "namespace"),
		"Label name to inject for namespace multi-tenancy (e.g., namespace, k8s_namespace)")

	flag.StringVar(&f_clusterLabel, "cluster-label",
		getenvOrDefault("DSPROXY_CLUSTER_LABEL", "cluster"),
		"Label name to inject for cluster multi-tenancy (e.g., cluster, k8s_cluster; empty disables cluster injection)")

	flag.StringVar(&f_jwtAudience, "jwt-audience",
		getenvOrDefault("DSPROXY_JWT_AUDIENCE", "example-app"),
		"Expected JWT audience claim")

	flag.StringVar(&f_caBundle, "ca-bundle",
		getenvOrDefault("DSPROXY_CA_BUNDLE", ""),
		"Path to CA bundle file for verifying upstream and JWKS certificates")

	flag.IntVar(&f_uiPort, "ui-port",
		getenvIntOrDefault("DSPROXY_UI_PORT", 3001),
		"Port to serve the UI")
}

func getenvOrDefault(envVar, fallback string) string {
	if val := os.Getenv(envVar); val != "" {
		return val
	}
	return fallback
}

func getenvIntOrDefault(envVar string, fallback int) int {
	if val := os.Getenv(envVar); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return fallback
}

func getenvBoolOrDefault(envVar string, fallback bool) bool {
	if val := os.Getenv(envVar); val != "" {
		return val == "1" || val == "true" || val == "TRUE"
	}
	return fallback
}

func startServers(authzService *AuthzService, promProxy http.Handler, tlsCert, tlsKey string) (httpServer, httpsServer *http.Server) {
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

	// Only start HTTPS server if TLS cert and key files exist. The keypair is
	// loaded synchronously so that certificate problems are reported at
	// startup instead of crashing the process from the server goroutine.
	if tlsCert != "" && tlsKey != "" {
		if _, err := os.Stat(tlsCert); err == nil {
			if _, err := os.Stat(tlsKey); err == nil {
				cert, err := tls.LoadX509KeyPair(tlsCert, tlsKey)
				if err != nil {
					log.Fatalf("HTTPS server error: failed to load TLS certificate: %v", err)
				}
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
					TLSConfig: &tls.Config{
						Certificates: []tls.Certificate{cert},
					},
				}
				log.Println("Starting HTTPS server on port", redirectPortHTTPS)
				go func() {
					// The certificate is already loaded into TLSConfig, so
					// the file paths are not used here.
					if err := httpsServer.ListenAndServeTLS("", ""); !errors.Is(err, http.ErrServerClosed) {
						log.Fatalf("HTTPS server error: %v", err)
					}
					log.Println("Stopped serving new HTTPS connections.")
				}()
			} else {
				log.Printf("TLS key file not found: %s - HTTPS server not started", tlsKey)
			}
		} else {
			log.Printf("TLS certificate file not found: %s - HTTPS server not started", tlsCert)
		}
	}
	return httpServer, httpsServer
}

func main() {
	flag.Parse()
	if f_iptables {
		log.Println("Iptables support enabled")
	}
	// Check if TLS files actually exist
	tlsAvailable := false
	if f_tlsCert != "" && f_tlsKey != "" {
		if _, err := os.Stat(f_tlsCert); err == nil {
			if _, err := os.Stat(f_tlsKey); err == nil {
				tlsAvailable = true
			}
		}
	}
	if tlsAvailable {
		log.Println("TLS support enabled")
	} else {
		log.Println("TLS support disabled (certificate files not found or not specified)")
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

	var iptMgr *iptablesManager
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
		ipt, err := iptables.New()
		if err != nil {
			log.Fatalf("Failed to initialize iptables: %v", err)
		}
		iptMgr = newIptablesManager(ipt)

		// Never intercept DSProxy's own connections to the upstream; the
		// REDIRECT rules would otherwise loop the proxy's traffic back into
		// itself.
		if upstreamURL, err := url.Parse(f_upstreamURL); err == nil && upstreamURL.Hostname() != "" {
			iptMgr.addSkipDomain(upstreamURL.Hostname())
		}

		cfg, err := loadConfig()
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
		iptMgr.apply(cfg)

		// Watch for config changes
		go watchConfig(ctx, f_configPath, func() {
			newCfg, err := loadConfig()
			if err != nil {
				log.Printf("Reload failed: %v", err)
				return
			}
			iptMgr.apply(newCfg)
		})

		// Periodically re-resolve domains so rules track DNS changes
		go watchDNS(ctx, iptMgr)
	}

	authzService, err := NewAuthzService(ctx, f_policyPath)
	if err != nil {
		log.Fatalf("Failed to initialize authorization service: %v", err)
	}

	// Initialize JWKS before serving requests
	if err := initJWKS(); err != nil {
		log.Fatalf("Failed to initialize JWKS: %v", err)
	}

	// Apply the CA bundle to the upstream transport if configured
	if err := configureUpstreamTLS(); err != nil {
		log.Fatalf("Failed to configure upstream TLS: %v", err)
	}

	// Create Prometheus proxy with label injection
	promProxy, _, err := newPrometheusProxy(f_upstreamURL, f_injectionLabel, f_clusterLabel)
	if err != nil {
		log.Fatalf("Failed to create Prometheus proxy: %v", err)
	}
	log.Printf("Prometheus proxy created: upstream=%s, labels=%s/%s", f_upstreamURL, f_clusterLabel, f_injectionLabel)

	httpServer, httpsServer := startServers(authzService, promProxy, f_tlsCert, f_tlsKey)
	uiServer := startUIServer(fmt.Sprintf("127.0.0.1:%d", f_uiPort), authzService)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	log.Println("Received shutdown signal")
	cancel() // Cancel context to stop watchers

	// Remove iptables rules before exiting
	if iptMgr != nil {
		iptMgr.cleanup()
	}

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
	if err := uiServer.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("UI server shutdown error: %v", err)
	}
	log.Println("Graceful shutdown complete.")
}
