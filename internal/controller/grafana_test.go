package controller

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
)

var _ = Describe("Grafana", func() {
	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}
	grafana := &grafoov1alpha1.Grafana{}
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
						Enabled: false,
					},
					TokenDuration: &metav1.Duration{Duration: time.Minute * 1440},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		}
	})
	AfterEach(func() {
		resource := &grafoov1alpha1.Grafana{}
		err := k8sClient.Get(ctx, typeNamespacedName, resource)
		Expect(err).NotTo(HaveOccurred())

		By("Cleanup the specific resource instance Grafana")
		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})
	Context("When reconciling a Grafana with missing cluster roles", func() {
		It("Should not fail due to missing cluster roles", func() {
			By("Checking cluster rolebindings")
			clusterRoleBinding := &rbacv1.ClusterRoleBinding{}
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-grafana-cluster-logging-application-view", Namespace: "default"}, clusterRoleBinding)
				g.Expect(err).To(HaveOccurred())
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
				return err
			}, 5*time.Second, 1*time.Second).Should(HaveOccurred())
			// cluster-logging-infrastructure-view
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-grafana-cluster-logging-infrastructure-view", Namespace: "default"}, clusterRoleBinding)
				g.Expect(err).To(HaveOccurred())
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
				return err
			}, 5*time.Second, 1*time.Second).Should(HaveOccurred())
			// cluster-logging-audit-view
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-grafana-cluster-logging-audit-view", Namespace: "default"}, clusterRoleBinding)
				g.Expect(err).To(HaveOccurred())
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
				return err
			}, 5*time.Second, 1*time.Second).Should(HaveOccurred())
		})
		It("Should not fail if cluster roles are found", func() {
			By("Creating cluster roles")
			clusterRoleBinding := &rbacv1.ClusterRoleBinding{}
			clusterRole := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-logging-application-view",
				},
			}
			Expect(k8sClient.Create(ctx, clusterRole)).To(Succeed())
			clusterRole = &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-logging-infrastructure-view",
				},
			}
			Expect(k8sClient.Create(ctx, clusterRole)).To(Succeed())
			clusterRole = &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-logging-audit-view",
				},
			}
			Expect(k8sClient.Create(ctx, clusterRole)).To(Succeed())
			By("Checking cluster rolebindings")
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-grafana-cluster-logging-application-view", Namespace: "default"}, clusterRoleBinding)
				g.Expect(err).NotTo(HaveOccurred())
				return err
			}, 5*time.Second, 1*time.Second).Should(Succeed())

			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-grafana-cluster-logging-infrastructure-view", Namespace: "default"}, clusterRoleBinding)
				g.Expect(err).NotTo(HaveOccurred())
				return err
			}, 5*time.Second, 1*time.Second).Should(Succeed())

			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-grafana-cluster-logging-audit-view", Namespace: "default"}, clusterRoleBinding)
				g.Expect(err).NotTo(HaveOccurred())
				return err
			}, 5*time.Second, 1*time.Second).Should(Succeed())
		})
	})
})
