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
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// --- Fake iptables for testing ---

type fakeIPTables struct {
	existsRules  map[string]bool
	appendCalled []string
	appendErr    error
	existsErr    error
}

func newFakeIPTables() *fakeIPTables {
	return &fakeIPTables{
		existsRules:  make(map[string]bool),
		appendCalled: []string{},
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
		origFTlsCert string
		origFTlsKey  string
	)

	BeforeEach(func() {
		// Generate temporary TLS cert and key for testing
		f_tlsCert, f_tlsKey = generateTempTLSFiles()
		// Store original values to restore later
		origFTlsCert = f_tlsCert
		origFTlsKey = f_tlsKey
	})

	AfterEach(func() {
		f_tlsCert = origFTlsCert
		f_tlsKey = origFTlsKey
	})

	It("should return a non-nil HTTP server", func() {
		f_tlsCert = ""
		f_tlsKey = ""
		authService := &AuthzService{}
		httpSrv, httpsSrv := startServers(authService)
		Expect(httpSrv).ToNot(BeNil())
		Expect(httpsSrv).To(BeNil())
		Expect(httpSrv.Addr).To(ContainSubstring(fmt.Sprintf("%d", redirectPortHTTP)))
		httpSrv.Close()

	})

	It("should return both HTTP and HTTPS servers if TLS cert and key are set", func() {
		authService := &AuthzService{}
		httpSrv, httpsSrv := startServers(authService)
		fmt.Fprintf(GinkgoWriter, "httpSrv: %+v, httpsSrv: %+v\n", httpSrv, httpsSrv)
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
		httpSrv, httpsSrv := startServers(authService)
		Expect(httpSrv).ToNot(BeNil())
		// The nil pointer dereference warning (SA5011) is about calling Close() on a possibly nil pointer.
		// The check `if httpSrv != nil { httpSrv.Close() }` is safe, but staticcheck warns that
		// if httpSrv is nil, calling Close() would panic. Since we just asserted it's not nil, this is safe.
		// However, if you remove the Expect(httpSrv).ToNot(BeNil()), you could get a nil pointer dereference.
		if httpSrv != nil {
			httpSrv.Close()
		}
		if httpsSrv != nil {
			httpsSrv.Close()
		}
	})
})

var _ = Describe("iptables rule management", func() {
	var fake *fakeIPTables

	BeforeEach(func() {
		fake = newFakeIPTables()
	})

	It("should detect existing iptables rule", func() {
		ip := "1.2.3.4"
		port := 80
		key := fmt.Sprintf("nat|OUTPUT|[%s %s %s %s %s %s %s %s %s %s]",
			"-p", "tcp", "-d", ip, "--dport", fmt.Sprintf("%d", port), "-j", "REDIRECT", "--to-port", fmt.Sprintf("%d", redirectPortHTTP))
		fake.existsRules[key] = true
		Expect(iptablesRuleExists(fake, ip, port)).To(BeTrue())
	})

	It("should add iptables rule if not exists", func() {
		ip := "1.2.3.4"
		port := 80
		Expect(addIptablesRule(fake, ip, port)).To(Succeed())
		Expect(fake.appendCalled).To(HaveLen(1))
	})

	It("should not add rule if already exists", func() {
		ip := "1.2.3.4"
		port := 80
		key := fmt.Sprintf("nat|OUTPUT|[%s %s %s %s %s %s %s %s %s %s]",
			"-p", "tcp", "-d", ip, "--dport", fmt.Sprintf("%d", port), "-j", "REDIRECT", "--to-port", fmt.Sprintf("%d", redirectPortHTTP))
		fake.existsRules[key] = true
		Expect(addIptablesRule(fake, ip, port)).To(Succeed())
		Expect(fake.appendCalled).To(HaveLen(0))
	})

	It("should handle error from Exists", func() {
		ip := "1.2.3.4"
		port := 80
		fake.existsErr = errors.New("fail")
		Expect(iptablesRuleExists(fake, ip, port)).To(BeFalse())
	})

	It("should return error if AppendUnique fails", func() {
		ip := "1.2.3.4"
		port := 80
		fake.appendErr = errors.New("append fail")
		Expect(addIptablesRule(fake, ip, port)).ToNot(Succeed())
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

var _ = Describe("applyRules", func() {
	var (
		fake                *fakeIPTables
		origResolveDomainIP func(string) (string, error)
	)

	BeforeEach(func() {
		fake = newFakeIPTables()
		origResolveDomainIP = resolveDomainIP
	})

	AfterEach(func() {
		resolveDomainIP = origResolveDomainIP
	})

	It("should add rules for all http and https ports", func() {
		resolveDomainIP = func(domain string) (string, error) {
			return "1.2.3.4", nil
		}
		cfg := &Config{
			Proxies: []ProxyRule{
				{
					Domain: "example.com",
					Proxies: []Proxies{
						{http: []int{80, 8080}, https: []int{443}},
					},
				},
			},
		}
		applyRules(fake, cfg)
		Expect(fake.appendCalled).To(HaveLen(3))
	})

	It("should skip rule if DNS fails", func() {
		resolveDomainIP = func(domain string) (string, error) {
			return "", errors.New("dns fail")
		}
		cfg := &Config{
			Proxies: []ProxyRule{
				{
					Domain: "bad.com",
					Proxies: []Proxies{
						{http: []int{80}, https: []int{443}},
					},
				},
			},
		}
		applyRules(fake, cfg)
		Expect(fake.appendCalled).To(BeEmpty())
	})

	It("should continue applying rules for multiple proxies even if one fails DNS", func() {
		resolveDomainIP = func(domain string) (string, error) {
			if domain == "fail.com" {
				return "", errors.New("dns fail")
			}
			return "5.6.7.8", nil
		}
		cfg := &Config{
			Proxies: []ProxyRule{
				{
					Domain: "fail.com",
					Proxies: []Proxies{
						{http: []int{80}},
					},
				},
				{
					Domain: "ok.com",
					Proxies: []Proxies{
						{http: []int{8080}, https: []int{8443}},
					},
				},
			},
		}
		applyRules(fake, cfg)
		Expect(fake.appendCalled).To(HaveLen(2))
	})

	It("should handle empty proxies list", func() {
		resolveDomainIP = func(domain string) (string, error) {
			return "1.2.3.4", nil
		}
		cfg := &Config{
			Proxies: []ProxyRule{
				{
					Domain:  "empty.com",
					Proxies: []Proxies{},
				},
			},
		}
		applyRules(fake, cfg)
		Expect(fake.appendCalled).To(BeEmpty())
	})
})
