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
			Expect(g.Spec.Version).To(Equal("10.4.3"))
			Expect(g.Spec.Dex).ToNot(BeNil())
			Expect(g.Spec.Dex.Enabled).To(BeTrue())
		})
	})

	Context("When creating Grafana under Validating Webhook", func() {
		It("Should deny if a required field is empty", func() {

			// TODO(user): Add your logic here

		})

		It("Should admit if all required fields are provided", func() {

			// TODO(user): Add your logic here

		})
	})

})
