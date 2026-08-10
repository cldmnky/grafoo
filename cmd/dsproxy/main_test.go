package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// --- Fake iptables for testing ---

type fakeIPTables struct {
	existsRules  map[string]bool
	appendCalled []string
	deleteCalled []string
	appendErr    error
	existsErr    error
	deleteErr    error
}

func newFakeIPTables() *fakeIPTables {
	return &fakeIPTables{
		existsRules:  make(map[string]bool),
		appendCalled: []string{},
		deleteCalled: []string{},
	}
}

func (f *fakeIPTables) Exists(table, chain string, rulespec ...string) (bool, error) {
	key := fmt.Sprintf("%s|%s|%v", table, chain, rulespec)
	if f.existsErr != nil {
		return false, f.existsErr
	}
	return f.existsRules[key], nil
}

func (f *fakeIPTables) AppendUnique(table, chain string, rulespec ...string) error {
	key := fmt.Sprintf("%s|%s|%v", table, chain, rulespec)
	f.appendCalled = append(f.appendCalled, key)
	return f.appendErr
}

func (f *fakeIPTables) Delete(table, chain string, rulespec ...string) error {
	key := fmt.Sprintf("%s|%s|%v", table, chain, rulespec)
	f.deleteCalled = append(f.deleteCalled, key)
	return f.deleteErr
}

func (f *fakeIPTables) appendedRuleSpecs() []string {
	specs := make([]string, 0, len(f.appendCalled))
	for _, c := range f.appendCalled {
		parts := strings.SplitN(c, "|", 3)
		specs = append(specs, parts[2])
	}
	return specs
}

func (f *fakeIPTables) deletedRuleSpecs() []string {
	specs := make([]string, 0, len(f.deleteCalled))
	for _, c := range f.deleteCalled {
		parts := strings.SplitN(c, "|", 3)
		specs = append(specs, parts[2])
	}
	return specs
}

// Generate a temporary TLS cert and key for testing
func generateTempTLSFiles() (string, string) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"TestOrg"},
		},
		NotBefore: time.Now().Add(-time.Hour),
		NotAfter:  time.Now().Add(time.Hour * 24),
		KeyUsage:  x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		panic(err)
	}

	certOut, err := os.CreateTemp("", "test-cert-*.crt")
	if err != nil {
		panic(err)
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		panic(err)
	}

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		panic(err)
	}
	keyOut, err := os.CreateTemp("", "test-key-*.key")
	if err != nil {
		panic(err)
	}
	defer keyOut.Close()
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		panic(err)
	}

	return certOut.Name(), keyOut.Name()
}

// --- Test Suite ---

func TestMain(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "dsproxy Suite")
}

var _ = BeforeSuite(func() {
	log.SetOutput(GinkgoWriter)
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.SetPrefix("dsproxy: ")
	log.Println("Starting dsproxy tests...")
})

var _ = Describe("startServers", func() {
	var (
		testCert, testKey string
	)

	BeforeEach(func() {
		// Generate temporary TLS cert and key for testing
		testCert, testKey = generateTempTLSFiles()
	})

	AfterEach(func() {
		os.Remove(testCert)
		os.Remove(testKey)
	})

	It("should return a non-nil HTTP server", func() {
		authService := &AuthzService{}
		mockProxy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		httpSrv, httpsSrv := startServers(authService, mockProxy, "", "")
		Expect(httpSrv).ToNot(BeNil())
		Expect(httpsSrv).To(BeNil())
		Expect(httpSrv.Addr).To(ContainSubstring(fmt.Sprintf("%d", redirectPortHTTP)))
		httpSrv.Close()
	})

	It("should return both HTTP and HTTPS servers if TLS cert and key are set", func() {
		authService := &AuthzService{}
		mockProxy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		httpSrv, httpsSrv := startServers(authService, mockProxy, testCert, testKey)
		Expect(httpSrv).ToNot(BeNil())
		Expect(httpsSrv).ToNot(BeNil())
		Expect(httpSrv.Addr).To(ContainSubstring(fmt.Sprintf("%d", redirectPortHTTP)))
		Expect(httpsSrv.Addr).To(ContainSubstring(fmt.Sprintf("%d", redirectPortHTTPS)))
		// Stop the servers to avoid port conflicts
		httpSrv.Close()
		httpsSrv.Close()
	})

	It("should use proxyHandler as handler for both servers", func() {
		authService := &AuthzService{}
		mockProxy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		httpSrv, httpsSrv := startServers(authService, mockProxy, testCert, testKey)
		Expect(httpSrv).ToNot(BeNil())
		if httpSrv != nil {
			httpSrv.Close()
		}
		if httpsSrv != nil {
			httpsSrv.Close()
		}
	})
})

var _ = Describe("config parsing", func() {
	var origConfigPath string

	BeforeEach(func() {
		origConfigPath = f_configPath
	})

	AfterEach(func() {
		f_configPath = origConfigPath
	})

	It("should parse the documented YAML shape", func() {
		dir := GinkgoT().TempDir()
		path := filepath.Join(dir, "dsproxy.yaml")
		content := `proxies:
  - domain: thanos-querier.openshift-monitoring.svc.cluster.local
    proxies:
      http: [9091]
      https: [9091]
  - domain: another.example.com
    proxies:
      http: [80, 8080]
      https: [443]
`
		Expect(os.WriteFile(path, []byte(content), 0644)).To(Succeed())
		f_configPath = path

		cfg, err := loadConfig()
		Expect(err).To(BeNil())
		Expect(cfg.Proxies).To(HaveLen(2))

		first := cfg.Proxies[0]
		Expect(first.Domain).To(Equal("thanos-querier.openshift-monitoring.svc.cluster.local"))
		Expect(first.Proxy.HTTP).To(Equal([]int{9091}))
		Expect(first.Proxy.HTTPS).To(Equal([]int{9091}))

		second := cfg.Proxies[1]
		Expect(second.Proxy.HTTP).To(Equal([]int{80, 8080}))
		Expect(second.Proxy.HTTPS).To(Equal([]int{443}))
	})

	It("should fail for a missing config file", func() {
		f_configPath = filepath.Join(GinkgoT().TempDir(), "does-not-exist.yaml")
		_, err := loadConfig()
		Expect(err).ToNot(BeNil())
	})

	It("should fail for invalid YAML", func() {
		dir := GinkgoT().TempDir()
		path := filepath.Join(dir, "dsproxy.yaml")
		Expect(os.WriteFile(path, []byte("proxies: [not valid"), 0644)).To(Succeed())
		f_configPath = path
		_, err := loadConfig()
		Expect(err).ToNot(BeNil())
	})
})

var _ = Describe("iptables rule management", func() {
	It("should build a rulespec with the given redirect port", func() {
		spec := ruleSpec("1.2.3.4", 443, redirectPortHTTPS)
		joined := strings.Join(spec, " ")
		Expect(joined).To(ContainSubstring("--to-port 5534"))
		Expect(joined).To(ContainSubstring("-d 1.2.3.4"))
		Expect(joined).To(ContainSubstring("--dport 443"))
	})

	It("should reject invalid ports", func() {
		Expect(validatePort(0)).ToNot(BeNil())
		Expect(validatePort(65536)).ToNot(BeNil())
		Expect(validatePort(80)).To(BeNil())
	})
})

var _ = Describe("resolveDomainIP", func() {
	It("should resolve a valid domain", func() {
		ip, err := resolveDomainIP("localhost")
		Expect(err).To(BeNil())
		Expect(net.ParseIP(ip)).ToNot(BeNil())
	})

	It("should fail for invalid domain", func() {
		_, err := resolveDomainIP("nonexistent.invalid.domain")
		Expect(err).ToNot(BeNil())
	})
})

var _ = Describe("iptablesManager", func() {
	var (
		fake                *fakeIPTables
		origResolveDomainIP func(string) (string, error)
	)

	BeforeEach(func() {
		fake = newFakeIPTables()
		origResolveDomainIP = resolveDomainIP
		resolveDomainIP = func(domain string) (string, error) {
			switch domain {
			case "fail.com":
				return "", errors.New("dns fail")
			default:
				return "1.2.3.4", nil
			}
		}
	})

	AfterEach(func() {
		resolveDomainIP = origResolveDomainIP
	})

	It("should redirect http ports to the HTTP listener and https ports to the HTTPS listener", func() {
		mgr := newIptablesManager(fake)
		cfg := &Config{
			Proxies: []ProxyRule{
				{
					Domain: "example.com",
					Proxy:  ProxyPorts{HTTP: []int{80, 8080}, HTTPS: []int{443}},
				},
			},
		}
		mgr.apply(cfg)

		specs := fake.appendedRuleSpecs()
		Expect(specs).To(HaveLen(3))
		var httpRedirects, httpsRedirects int
		for _, s := range specs {
			if strings.Contains(s, "--to-port 5533") {
				httpRedirects++
			}
			if strings.Contains(s, "--to-port 5534") {
				httpsRedirects++
			}
		}
		Expect(httpRedirects).To(Equal(2))
		Expect(httpsRedirects).To(Equal(1))
	})

	It("should be idempotent for identical configurations", func() {
		mgr := newIptablesManager(fake)
		cfg := &Config{
			Proxies: []ProxyRule{
				{
					Domain: "example.com",
					Proxy:  ProxyPorts{HTTP: []int{80}, HTTPS: []int{443}},
				},
			},
		}
		mgr.apply(cfg)
		mgr.apply(cfg)
		Expect(fake.appendCalled).To(HaveLen(2))
	})

	It("should remove stale rules when a proxy entry is removed from the config", func() {
		mgr := newIptablesManager(fake)
		cfg1 := &Config{
			Proxies: []ProxyRule{
				{
					Domain: "example.com",
					Proxy:  ProxyPorts{HTTP: []int{80}},
				},
			},
		}
		mgr.apply(cfg1)
		Expect(fake.appendCalled).To(HaveLen(1))

		mgr.apply(&Config{})
		Expect(fake.deleteCalled).To(HaveLen(1))
		Expect(fake.deletedRuleSpecs()[0]).To(ContainSubstring("--to-port 5533"))
	})

	It("should skip rules for domains that fail DNS resolution", func() {
		mgr := newIptablesManager(fake)
		cfg := &Config{
			Proxies: []ProxyRule{
				{
					Domain: "fail.com",
					Proxy:  ProxyPorts{HTTP: []int{80}, HTTPS: []int{443}},
				},
			},
		}
		mgr.apply(cfg)
		Expect(fake.appendCalled).To(BeEmpty())
	})

	It("should skip invalid ports", func() {
		mgr := newIptablesManager(fake)
		cfg := &Config{
			Proxies: []ProxyRule{
				{
					Domain: "example.com",
					Proxy:  ProxyPorts{HTTP: []int{0, 80}, HTTPS: []int{70000}},
				},
			},
		}
		mgr.apply(cfg)
		Expect(fake.appendCalled).To(HaveLen(1))
		Expect(fake.appendedRuleSpecs()[0]).To(ContainSubstring("--dport 80"))
	})

	It("should clean up all installed rules", func() {
		mgr := newIptablesManager(fake)
		cfg := &Config{
			Proxies: []ProxyRule{
				{
					Domain: "example.com",
					Proxy:  ProxyPorts{HTTP: []int{80}, HTTPS: []int{443}},
				},
			},
		}
		mgr.apply(cfg)
		Expect(fake.appendCalled).To(HaveLen(2))

		mgr.cleanup()
		Expect(fake.deleteCalled).To(HaveLen(2))
		Expect(mgr.installed).To(BeEmpty())

		// Second cleanup is a no-op
		fake.deleteCalled = []string{}
		mgr.cleanup()
		Expect(fake.deleteCalled).To(BeEmpty())
	})

	It("should apply rules for multiple proxies even if one fails DNS", func() {
		mgr := newIptablesManager(fake)
		cfg := &Config{
			Proxies: []ProxyRule{
				{
					Domain: "fail.com",
					Proxy:  ProxyPorts{HTTP: []int{80}},
				},
				{
					Domain: "ok.com",
					Proxy:  ProxyPorts{HTTP: []int{8080}, HTTPS: []int{8443}},
				},
			},
		}
		mgr.apply(cfg)
		Expect(fake.appendCalled).To(HaveLen(2))
	})

	It("should skip rules for the upstream domain to avoid self-interception", func() {
		mgr := newIptablesManager(fake)
		mgr.addSkipDomain("thanos-querier.example.com")
		cfg := &Config{
			Proxies: []ProxyRule{
				{
					Domain: "thanos-querier.example.com",
					Proxy:  ProxyPorts{HTTP: []int{9091}},
				},
				{
					Domain: "other.example.com",
					Proxy:  ProxyPorts{HTTP: []int{80}},
				},
			},
		}
		mgr.apply(cfg)
		Expect(fake.appendCalled).To(HaveLen(1))
		Expect(fake.appendedRuleSpecs()[0]).To(ContainSubstring("--dport 80"))
	})
})
