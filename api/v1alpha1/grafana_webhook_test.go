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
			Expect(g.Spec.Version).To(Equal(GrafanaVersion))
			Expect(g.Spec.Dex).ToNot(BeNil())
			Expect(g.Spec.Dex.Enabled).To(BeTrue())
			Expect(g.Spec.Dex.Image).To(Equal(DexImage))
			Expect(g.Spec.Replicas).ToNot(BeNil())
			Expect(*g.Spec.Replicas).To(Equal(GrafanaReplicas))
			Expect(g.Spec.DataSources).ToNot(BeNil())
			Expect(g.Spec.DataSources).To(Equal(DataSources))
			Expect(g.Spec.TokenDuration).To(Equal(TokenDuration))

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
							URL:     "http://prometheus.openshift-monitoring.svc.cluster.local:9090",
							Enabled: true,
						},
					},
				},
			}
			warn, err := g.ValidateCreate()
			Expect(err).To(BeNil())
			Expect(warn).ToNot(BeNil())

		})

		It("Should admit if all required fields are provided", func() {

			// TODO(user): Add your logic here

		})
	})

})
