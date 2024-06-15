package controller

import (
	"context"
	"time"

	grafanav1beta1 "github.com/grafana/grafana-operator/v5/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
)

var _ = Describe("Datasource Controller", func() {
	Context("When reconciling a datasource", func() {
		const resourceName = "test-grafana"

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
						TokenDuration: metav1.Duration{Duration: time.Minute * 10},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
			By("creating a cluster ingress object")
			ingress := &configv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Spec: configv1.IngressSpec{
					Domain: "apps.foo.bar",
				},
			}
			Expect(k8sClient.Create(ctx, ingress)).To(Succeed())

			By("creating a service account")
			serviceAccount := &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName + "-sa",
					Namespace: "default",
				},
			}
			Expect(k8sClient.Create(ctx, serviceAccount)).To(Succeed())
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &grafoov1alpha1.Grafana{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance Grafana")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			By("Cleanup the specific resource instance cluster ingress")
			ingress := &configv1.Ingress{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, ingress)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Delete(ctx, ingress)).To(Succeed())

			By("Cleanup the specific resource instance service account")
			serviceAccount := &corev1.ServiceAccount{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-sa", Namespace: "default"}, serviceAccount)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Delete(ctx, serviceAccount)).To(Succeed())
		})
		It("should successfully create a data source", func() {
			// Get the Grafana instance
			err := k8sClient.Get(ctx, typeNamespacedName, grafana)
			Expect(err).NotTo(HaveOccurred())
			// Add a data source to the Grafana instance
			grafana.Spec.DataSources = []grafoov1alpha1.DataSource{
				{
					Name:    "Prometheus",
					Type:    "prometheus-incluster",
					URL:     "http://prometheus.openshift-monitoring.svc.cluster.local:9090",
					Enabled: true,
				},
			}
			Expect(k8sClient.Update(ctx, grafana)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &GrafanaReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Clientset: clientSet,
			}

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Get the Grafana instance
			err = k8sClient.Get(ctx, typeNamespacedName, grafana)
			// Get there ds hash name
			dsHashName := grafana.Spec.DataSources[0].GetDataSourceNameHash()
			Expect(err).NotTo(HaveOccurred())
			By("Checking the created GrafanaDatasource")
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      resourceName + "-" + dsHashName,
				Namespace: "default",
			}, grafanaOperatedDS)
			Expect(err).NotTo(HaveOccurred())

		})
	})
})
