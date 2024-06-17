package v1alpha1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Grafana Webhook", func() {
	Context("When creating Grafana under Defaulting Webhook", func() {
		It("Should fill in the default value if a required field is empty", func() {
			g := &Grafana{
				Spec: GrafanaSpec{
					Version: "",
					Dex:     nil,
				},
			}
			g.Default()
			Expect(g.Spec.Dex).ToNot(BeNil())
			Expect(g.Spec.Dex.Enabled).To(BeTrue())
			Expect(g.Spec.Dex.Image).To(Equal(DexImage))
			Expect(g.Spec.Replicas).ToNot(BeNil())
			Expect(*g.Spec.Replicas).To(Equal(GrafanaReplicas))
			Expect(g.Spec.DataSources).ToNot(BeNil())
			Expect(g.Spec.DataSources).To(Equal(DataSources))
		})
	})

	Context("When creating Grafana under Validating Webhook", func() {
		It("Should deny if a a data source type is not allowed", func() {
			g := &Grafana{
				Spec: GrafanaSpec{
					DataSources: []DataSource{
						{
							Name:    "prometheus",
							Type:    "foo-type",
							Enabled: true,
						},
					},
				},
			}
			warn, err := g.ValidateCreate()
			Expect(err).NotTo(BeNil())
			Expect(warn).To(BeNil())

		})

		It("Should deny if a data source type does not have a struct", func() {
			g := &Grafana{
				Spec: GrafanaSpec{
					DataSources: []DataSource{
						{
							Name:    "foo-name",
							Type:    "prometheus-incluster",
							Enabled: true,
						},
					},
				},
			}
			warn, err := g.ValidateCreate()
			Expect(err).NotTo(BeNil())
			Expect(warn).To(BeNil())

		})

		It("Should deny if a data source type does not have a correct struct", func() {
			g := &Grafana{
				Spec: GrafanaSpec{
					DataSources: []DataSource{
						{
							Name:    "foo-name",
							Type:    "prometheus-incluster",
							Enabled: true,
							Loki: &LokiDS{
								URL: "http://prometheus.monitoring.svc",
							},
						},
					},
				},
			}
			warn, err := g.ValidateCreate()
			Expect(err).NotTo(BeNil())
			Expect(warn).To(BeNil())
		})

		It("Should deny if a data source type have extra structs", func() {
			g := &Grafana{
				Spec: GrafanaSpec{
					DataSources: []DataSource{
						{
							Name:    "foo-name",
							Type:    "prometheus-incluster",
							Enabled: true,
							Prometheus: &PrometheusDS{
								URL: "http://prometheus.monitoring.svc",
							},
							Loki: &LokiDS{
								URL: "http://loki.monitoring.svc",
							},
						},
					},
				},
			}
			warn, err := g.ValidateCreate()
			Expect(err).NotTo(BeNil())
			Expect(warn).To(BeNil())
		})

		It("Should deny if a data source type have missing required fields", func() {
			g := &Grafana{
				Spec: GrafanaSpec{
					DataSources: []DataSource{
						{
							Name:       "foo-name",
							Type:       "prometheus-incluster",
							Enabled:    true,
							Prometheus: &PrometheusDS{},
						},
					},
				},
			}
			warn, err := g.ValidateCreate()
			Expect(err).NotTo(BeNil())
			Expect(warn).To(BeNil())
		})

		It("Should admit if all required fields are provided", func() {

			// TODO(user): Add your logic here

		})
	})

})
