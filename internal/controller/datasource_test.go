package controller

import (
	"context"
	"time"

	grafanav1beta1 "github.com/grafana/grafana-operator/v5/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

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
			}, time.Minute, time.Second).Should(Succeed())
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
			}, time.Minute, time.Second).Should(Succeed())
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
			}, time.Minute, time.Second).Should(Succeed())
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
			}, time.Minute, time.Second).Should(Succeed())
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, typeNamespacedName, grafana)
				g.Expect(err).NotTo(HaveOccurred())
				grafana.Spec.DataSources = []grafoov1alpha1.DataSource{}
				err = k8sClient.Update(ctx, grafana)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}, time.Minute, time.Second).Should(Succeed())
			Expect(err).NotTo(HaveOccurred())
			By("Checking the grafana instance")
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, typeNamespacedName, grafana)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}, time.Minute, time.Second).Should(Succeed())
			By("Checking the deleted GrafanaDatasource")
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      resourceName + "-" + dsHashName,
					Namespace: "default",
				}, grafanaOperatedDS)
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
				return nil
			}, time.Minute, time.Second).Should(Succeed())
		})
	})
})
