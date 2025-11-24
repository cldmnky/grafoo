package controller

import (
	"context"
	"time"

	grafanav1beta1 "github.com/grafana/grafana-operator/v5/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
)

var _ = Describe("Datasource Controller", func() {
	Context("When reconciling a datasource", func() {

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		grafana := &grafoov1alpha1.Grafana{}
		grafanaOperatedDS := &grafanav1beta1.GrafanaDatasource{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Grafana")
			err := k8sClient.Get(ctx, typeNamespacedName, grafana)
			if err != nil && errors.IsNotFound(err) {
				resource := &grafoov1alpha1.Grafana{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: grafoov1alpha1.GrafanaSpec{
						Dex: &grafoov1alpha1.Dex{
							Enabled: true,
						},
						MariaDB: &grafoov1alpha1.MariaDB{
							Enabled:     false,
							StorageSize: "1Gi",
							Image:       grafoov1alpha1.MariaDBImage,
						},
						EnableDSProxy: true,
						TokenDuration: &metav1.Duration{Duration: time.Minute * 1440},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &grafoov1alpha1.Grafana{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance Grafana")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

		})
		It("should successfully create a data source", func() {
			// Get the Grafana instance
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, typeNamespacedName, grafana)
				g.Expect(err).NotTo(HaveOccurred())
				// Add a data source to the Grafana instance
				grafana.Spec.DataSources = []grafoov1alpha1.DataSource{
					{
						Name:    "Prometheus",
						Type:    "prometheus-incluster",
						Enabled: true,
						Prometheus: &grafoov1alpha1.PrometheusDS{
							URL: "http://prometheus.default.svc.cluster.local",
						},
					},
				}
				err = k8sClient.Update(ctx, grafana)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}, time.Second*10, time.Second).Should(Succeed())
			// Get the Grafana instance
			err := k8sClient.Get(ctx, typeNamespacedName, grafana)
			dsHashName := grafana.Spec.DataSources[0].GetDataSourceNameHash()
			Expect(err).NotTo(HaveOccurred())
			By("Checking the created GrafanaDatasource")
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      resourceName + "-" + dsHashName,
					Namespace: "default",
				}, grafanaOperatedDS)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}, time.Second*10, time.Second).Should(Succeed())
		})
		It("should successfully delete a data source", func() {
			// Get the Grafana instance
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, typeNamespacedName, grafana)
				g.Expect(err).NotTo(HaveOccurred())
				// Add a data source to the Grafana instance
				grafana.Spec.DataSources = []grafoov1alpha1.DataSource{
					{
						Name:    "Prometheus",
						Type:    "prometheus-incluster",
						Enabled: true,
						Prometheus: &grafoov1alpha1.PrometheusDS{
							URL: "http://prometheus.default.svc.cluster.local",
						},
					},
				}
				err = k8sClient.Update(ctx, grafana)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}, time.Second*10, time.Second).Should(Succeed())
			// Get the Grafana instance
			err := k8sClient.Get(ctx, typeNamespacedName, grafana)
			dsHashName := grafana.Spec.DataSources[0].GetDataSourceNameHash()
			Expect(err).NotTo(HaveOccurred())
			By("Checking the created GrafanaDatasource")
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      resourceName + "-" + dsHashName,
					Namespace: "default",
				}, grafanaOperatedDS)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}, time.Second*10, time.Second).Should(Succeed())
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, typeNamespacedName, grafana)
				g.Expect(err).NotTo(HaveOccurred())
				grafana.Spec.DataSources = []grafoov1alpha1.DataSource{}
				err = k8sClient.Update(ctx, grafana)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}, time.Second*10, time.Second).Should(Succeed())
			Expect(err).NotTo(HaveOccurred())
			By("Checking the grafana instance")
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, typeNamespacedName, grafana)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}, time.Second*10, time.Second).Should(Succeed())
			By("Checking the deleted GrafanaDatasource")
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      resourceName + "-" + dsHashName,
					Namespace: "default",
				}, grafanaOperatedDS)
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
				return nil
			}, time.Second*10, time.Second).Should(Succeed())
		})
		It("should successfully create a dsproxy ConfigMap with correct configuration", func() {
			// Get the Grafana instance and add datasources
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, typeNamespacedName, grafana)
				g.Expect(err).NotTo(HaveOccurred())
				// Add multiple datasources to test configuration generation
				grafana.Spec.DataSources = []grafoov1alpha1.DataSource{
					{
						Name:    "Prometheus",
						Type:    "prometheus-incluster",
						Enabled: true,
						Prometheus: &grafoov1alpha1.PrometheusDS{
							URL: "http://prometheus.default.svc.cluster.local:9090",
						},
					},
					{
						Name:    "Loki",
						Type:    "loki-incluster",
						Enabled: true,
						Loki: &grafoov1alpha1.LokiDS{
							URL: "https://loki.openshift-logging.svc.cluster.local:3100",
						},
					},
				}
				err = k8sClient.Update(ctx, grafana)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}, time.Second*10, time.Second).Should(Succeed())

			// Wait for the datasources to be created
			promDSHashName := grafana.Spec.DataSources[0].GetDataSourceNameHash()
			lokiDSHashName := grafana.Spec.DataSources[1].GetDataSourceNameHash()

			By("Waiting for Prometheus datasource to be created")
			grafanaOperatedPromDS := &grafanav1beta1.GrafanaDatasource{}
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      resourceName + "-" + promDSHashName,
					Namespace: "default",
				}, grafanaOperatedPromDS)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}, time.Second*10, time.Second).Should(Succeed())

			By("Waiting for Loki datasource to be created")
			grafanaOperatedLokiDS := &grafanav1beta1.GrafanaDatasource{}
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      resourceName + "-" + lokiDSHashName,
					Namespace: "default",
				}, grafanaOperatedLokiDS)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}, time.Second*10, time.Second).Should(Succeed())

			// Wait for the ConfigMap to be created with correct content
			By("Waiting for dsproxy ConfigMap to be created with correct configuration")
			dsproxyConfigMap := &corev1.ConfigMap{}
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      resourceName + "-dsproxy-config",
					Namespace: "default",
				}, dsproxyConfigMap)
				g.Expect(err).NotTo(HaveOccurred())

				// Verify the ConfigMap has the correct data
				g.Expect(dsproxyConfigMap.Data).To(HaveKey("dsproxy.yaml"))

				// Parse the YAML and verify structure
				var config DSProxyConfig
				err = yaml.Unmarshal([]byte(dsproxyConfigMap.Data["dsproxy.yaml"]), &config)
				g.Expect(err).NotTo(HaveOccurred())

				// Should have 2 proxy rules (one for Prometheus, one for Loki)
				g.Expect(config.Proxies).To(HaveLen(2), "Expected 2 proxy rules in dsproxy config")

				return nil
			}, time.Second*10, time.Second).Should(Succeed())

			// Verify the domains are present
			var config DSProxyConfig
			err := yaml.Unmarshal([]byte(dsproxyConfigMap.Data["dsproxy.yaml"]), &config)
			Expect(err).NotTo(HaveOccurred())

			domains := make([]string, len(config.Proxies))
			for i, proxy := range config.Proxies {
				domains[i] = proxy.Domain
			}
			Expect(domains).To(ContainElements(
				"prometheus.default.svc.cluster.local",
				"loki.openshift-logging.svc.cluster.local",
			))

			// Find and verify Prometheus configuration
			var promConfig *DSProxyRule
			for i := range config.Proxies {
				if config.Proxies[i].Domain == "prometheus.default.svc.cluster.local" {
					promConfig = &config.Proxies[i]
					break
				}
			}
			Expect(promConfig).NotTo(BeNil())
			Expect(promConfig.Proxies).To(HaveLen(1))
			Expect(promConfig.Proxies[0].HTTP).To(ContainElement(9090))

			// Find and verify Loki configuration
			var lokiConfig *DSProxyRule
			for i := range config.Proxies {
				if config.Proxies[i].Domain == "loki.openshift-logging.svc.cluster.local" {
					lokiConfig = &config.Proxies[i]
					break
				}
			}
			Expect(lokiConfig).NotTo(BeNil())
			Expect(lokiConfig.Proxies).To(HaveLen(1))
			Expect(lokiConfig.Proxies[0].HTTPS).To(ContainElement(3100))
		})
	})
})

var _ = Describe("extractTokenFromSecureJSONData", func() {
	Context("When extracting token from secureJSONData", func() {
		It("should extract token with Bearer prefix", func() {
			jsonData := []byte(`{"httpHeaderValue1": "Bearer mytoken123"}`)
			token, err := extractTokenFromSecureJSONData(jsonData)
			Expect(err).NotTo(HaveOccurred())
			Expect(token).To(Equal("mytoken123"))
		})

		It("should not extract token without Bearer prefix", func() {
			jsonData := []byte(`{"httpHeaderValue1": "mytoken123"}`)
			_, err := extractTokenFromSecureJSONData(jsonData)
			Expect(err).To(HaveOccurred())
		})

		It("should return error when token key is missing", func() {
			jsonData := []byte(`{"someOtherKey": "value"}`)
			_, err := extractTokenFromSecureJSONData(jsonData)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("token not found in secureJSONData"))
		})

		It("should return error with invalid JSON", func() {
			jsonData := []byte(`{invalid json`)
			_, err := extractTokenFromSecureJSONData(jsonData)
			Expect(err).To(HaveOccurred())
		})
	})
})

var _ = Describe("parseURLHostPort", func() {
	Context("When parsing URLs", func() {
		It("should parse HTTP URL with explicit port", func() {
			hostname, port, scheme, err := parseURLHostPort("http://prometheus.default.svc.cluster.local:9090")
			Expect(err).NotTo(HaveOccurred())
			Expect(hostname).To(Equal("prometheus.default.svc.cluster.local"))
			Expect(port).To(Equal(9090))
			Expect(scheme).To(Equal("http"))
		})

		It("should parse HTTPS URL with explicit port", func() {
			hostname, port, scheme, err := parseURLHostPort("https://loki.openshift-logging.svc.cluster.local:3100")
			Expect(err).NotTo(HaveOccurred())
			Expect(hostname).To(Equal("loki.openshift-logging.svc.cluster.local"))
			Expect(port).To(Equal(3100))
			Expect(scheme).To(Equal("https"))
		})

		It("should use default HTTP port when not specified", func() {
			hostname, port, scheme, err := parseURLHostPort("http://prometheus.default.svc.cluster.local")
			Expect(err).NotTo(HaveOccurred())
			Expect(hostname).To(Equal("prometheus.default.svc.cluster.local"))
			Expect(port).To(Equal(80))
			Expect(scheme).To(Equal("http"))
		})

		It("should use default HTTPS port when not specified", func() {
			hostname, port, scheme, err := parseURLHostPort("https://loki.openshift-logging.svc.cluster.local")
			Expect(err).NotTo(HaveOccurred())
			Expect(hostname).To(Equal("loki.openshift-logging.svc.cluster.local"))
			Expect(port).To(Equal(443))
			Expect(scheme).To(Equal("https"))
		})

		It("should return error for URL without scheme", func() {
			// URLs without a scheme like "prometheus.default.svc.cluster.local:9090"
			// are parsed incorrectly by url.Parse - it treats "prometheus.default.svc.cluster.local"
			// as the scheme and "9090" as the path
			_, _, _, err := parseURLHostPort("prometheus.default.svc.cluster.local:9090")
			Expect(err).To(HaveOccurred())
		})

		It("should return error for invalid URL", func() {
			_, _, _, err := parseURLHostPort("://invalid-url")
			Expect(err).To(HaveOccurred())
		})

		It("should return error for URL with invalid port", func() {
			_, _, _, err := parseURLHostPort("http://prometheus.default.svc.cluster.local:invalid")
			Expect(err).To(HaveOccurred())
		})

		It("should return error for URL with invalid scheme", func() {
			_, _, _, err := parseURLHostPort("ftp://prometheus.default.svc.cluster.local:9090")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("scheme must be http or https"))
		})
	})
})

var _ = Describe("buildDSProxyConfig", func() {
	Context("When building dsproxy config", func() {
		var (
			reconciler *GrafanaReconciler
			ctx        context.Context
		)

		BeforeEach(func() {
			reconciler = &GrafanaReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			ctx = context.Background()
		})

		It("should build config with single Prometheus datasource", func() {
			instance := &grafoov1alpha1.Grafana{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-grafana",
					Namespace: "default",
				},
				Spec: grafoov1alpha1.GrafanaSpec{
					DataSources: []grafoov1alpha1.DataSource{
						{
							Name:    "Prometheus",
							Type:    grafoov1alpha1.PrometheusInCluster,
							Enabled: true,
							Prometheus: &grafoov1alpha1.PrometheusDS{
								URL: "http://prometheus.default.svc.cluster.local:9090",
							},
						},
					},
				},
			}

			config, err := reconciler.buildDSProxyConfig(ctx, instance)
			Expect(err).NotTo(HaveOccurred())
			Expect(config.Proxies).To(HaveLen(1))
			Expect(config.Proxies[0].Domain).To(Equal("prometheus.default.svc.cluster.local"))
			Expect(config.Proxies[0].Proxies).To(HaveLen(1))
			Expect(config.Proxies[0].Proxies[0].HTTP).To(ContainElement(9090))
		})

		It("should build config with multiple datasources", func() {
			instance := &grafoov1alpha1.Grafana{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-grafana",
					Namespace: "default",
				},
				Spec: grafoov1alpha1.GrafanaSpec{
					DataSources: []grafoov1alpha1.DataSource{
						{
							Name:    "Prometheus",
							Type:    grafoov1alpha1.PrometheusInCluster,
							Enabled: true,
							Prometheus: &grafoov1alpha1.PrometheusDS{
								URL: "http://prometheus.default.svc.cluster.local:9090",
							},
						},
						{
							Name:    "Loki",
							Type:    grafoov1alpha1.LokiInCluster,
							Enabled: true,
							Loki: &grafoov1alpha1.LokiDS{
								URL: "https://loki.openshift-logging.svc.cluster.local:3100",
							},
						},
						{
							Name:    "Tempo",
							Type:    grafoov1alpha1.TempoInCluster,
							Enabled: true,
							Tempo: &grafoov1alpha1.TempoDS{
								URL: "https://tempo.tempo-system.svc.cluster.local:3200",
							},
						},
					},
				},
			}

			config, err := reconciler.buildDSProxyConfig(ctx, instance)
			Expect(err).NotTo(HaveOccurred())
			Expect(config.Proxies).To(HaveLen(3))
		})

		It("should skip disabled datasources", func() {
			instance := &grafoov1alpha1.Grafana{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-grafana",
					Namespace: "default",
				},
				Spec: grafoov1alpha1.GrafanaSpec{
					DataSources: []grafoov1alpha1.DataSource{
						{
							Name:    "Prometheus",
							Type:    grafoov1alpha1.PrometheusInCluster,
							Enabled: true,
							Prometheus: &grafoov1alpha1.PrometheusDS{
								URL: "http://prometheus.default.svc.cluster.local:9090",
							},
						},
						{
							Name:    "Loki",
							Type:    grafoov1alpha1.LokiInCluster,
							Enabled: false,
							Loki: &grafoov1alpha1.LokiDS{
								URL: "https://loki.openshift-logging.svc.cluster.local:3100",
							},
						},
					},
				},
			}

			config, err := reconciler.buildDSProxyConfig(ctx, instance)
			Expect(err).NotTo(HaveOccurred())
			Expect(config.Proxies).To(HaveLen(1))
			Expect(config.Proxies[0].Domain).To(Equal("prometheus.default.svc.cluster.local"))
		})

		It("should group multiple ports for same domain", func() {
			instance := &grafoov1alpha1.Grafana{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-grafana",
					Namespace: "default",
				},
				Spec: grafoov1alpha1.GrafanaSpec{
					DataSources: []grafoov1alpha1.DataSource{
						{
							Name:    "Prometheus-1",
							Type:    grafoov1alpha1.PrometheusInCluster,
							Enabled: true,
							Prometheus: &grafoov1alpha1.PrometheusDS{
								URL: "http://prometheus.default.svc.cluster.local:9090",
							},
						},
						{
							Name:    "Prometheus-2",
							Type:    grafoov1alpha1.PrometheusInCluster,
							Enabled: true,
							Prometheus: &grafoov1alpha1.PrometheusDS{
								URL: "https://prometheus.default.svc.cluster.local:9091",
							},
						},
					},
				},
			}

			config, err := reconciler.buildDSProxyConfig(ctx, instance)
			Expect(err).NotTo(HaveOccurred())
			Expect(config.Proxies).To(HaveLen(1))
			Expect(config.Proxies[0].Domain).To(Equal("prometheus.default.svc.cluster.local"))
			Expect(config.Proxies[0].Proxies).To(HaveLen(1))
			Expect(config.Proxies[0].Proxies[0].HTTP).To(ContainElement(9090))
			Expect(config.Proxies[0].Proxies[0].HTTPS).To(ContainElement(9091))
		})

		It("should return empty config when no enabled datasources", func() {
			instance := &grafoov1alpha1.Grafana{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-grafana",
					Namespace: "default",
				},
				Spec: grafoov1alpha1.GrafanaSpec{
					DataSources: []grafoov1alpha1.DataSource{},
				},
			}

			config, err := reconciler.buildDSProxyConfig(ctx, instance)
			Expect(err).NotTo(HaveOccurred())
			Expect(config.Proxies).To(BeEmpty())
		})
	})
})
