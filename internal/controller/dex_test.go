package controller

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus/testutil"
	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
)

var _ = Describe("Dex", func() {

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
						Enabled:     true,
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
		resource := &grafoov1alpha1.Grafana{}
		err := k8sClient.Get(ctx, typeNamespacedName, resource)
		Expect(err).NotTo(HaveOccurred())

		By("Cleanup the specific resource instance Grafana")
		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})
	Context("When reconciling a Grafana with Dex enabled", func() {
		It("Should create a client secret", func() {
			By("Getting the secret")
			clientSecretFirst := &corev1.Secret{}
			clientSecretTypeNamespacedName := types.NamespacedName{
				Name:      fmt.Sprintf("%s-dex-client-secret", resourceName),
				Namespace: "default",
			}
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, clientSecretTypeNamespacedName, clientSecretFirst)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}, time.Minute, time.Second).Should(Succeed())
			Expect(clientSecretFirst.Data).To(HaveKey("clientSecret"))

			By("Triggering a reconciliation to ensure the secret is not recreated")
			// Trigger a reconciliation to ensure the secret is not recreated
			// Simply add a label to the Grafana instance
			Eventually(func(g Gomega) error {
				// Get the Grafana instance
				err := k8sClient.Get(ctx, typeNamespacedName, grafana)
				g.Expect(err).NotTo(HaveOccurred())
				// Update the Grafana instance with the new label
				grafana.Labels = map[string]string{"test": "test"}
				err = k8sClient.Update(ctx, grafana)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}, time.Minute, time.Second).Should(Succeed())
			By("Getting the secret again")
			clientSecretSecond := &corev1.Secret{}
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, clientSecretTypeNamespacedName, clientSecretSecond)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}, time.Minute, time.Second).Should(Succeed())
			By("Checking if the client secret is the same")
			Expect(clientSecretFirst).To(Equal(clientSecretSecond))
			// Resource version should be the same
			Expect(clientSecretFirst.ResourceVersion).To(Equal(clientSecretSecond.ResourceVersion))
			// Sould have a controller reference
			Expect(clientSecretFirst.OwnerReferences).To(HaveLen(1))
			Expect(clientSecretFirst.OwnerReferences[0].Kind).To(Equal("Grafana"))
		})
		It("Should create a service account", func() {
			By("Getting the service account")
			serviceAccount := &corev1.ServiceAccount{}
			serviceAccountTypeNamespacedName := types.NamespacedName{
				Name:      fmt.Sprintf("%s-dex", resourceName),
				Namespace: "default",
			}
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, serviceAccountTypeNamespacedName, serviceAccount)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}, time.Minute, time.Second).Should(Succeed())
			Expect(serviceAccount.Annotations).To(HaveKey("serviceaccounts.openshift.io/oauth-redirecturi.dex"))
			Expect(serviceAccount.Annotations["serviceaccounts.openshift.io/oauth-redirecturi.dex"]).To(Equal("https://test-grafana-dex-default.apps.foo.bar/callback"))
			// Owner reference should be set
			Expect(serviceAccount.OwnerReferences).To(HaveLen(1))
			Expect(serviceAccount.OwnerReferences[0].Kind).To(Equal("Grafana"))
		})
		It("Should create a dex secret with configuration", func() {
			By("Getting the Dex secret")
			dexSecret := &corev1.Secret{}
			dexSecretTypeNamespacedName := types.NamespacedName{
				Name:      fmt.Sprintf("%s-dex", resourceName),
				Namespace: "default",
			}
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, dexSecretTypeNamespacedName, dexSecret)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}, time.Minute, time.Second).Should(Succeed())
			Expect(dexSecret.Data).To(HaveKey("config.yaml"))
			// Owner reference should be set
			Expect(dexSecret.OwnerReferences).To(HaveLen(1))
			Expect(dexSecret.OwnerReferences[0].Kind).To(Equal("Grafana"))
			obj := make(map[string]interface{})
			yaml.Unmarshal(dexSecret.Data["config.yaml"], &obj)
			// config
			Expect(obj).To(HaveKey("connectors"))
			connectors := obj["connectors"].([]interface{})
			Expect(connectors).To(HaveLen(1))
			connector := connectors[0].(map[string]interface{})
			// config
			Expect(connector).To(HaveKey("id"))
			Expect(connector["id"]).To(Equal("openshift"))
			Expect(connector).To(HaveKey("name"))
			Expect(connector["name"]).To(Equal("OpenShift"))
			Expect(connector).To(HaveKey("type"))
			Expect(connector["type"]).To(Equal("openshift"))
			Expect(connector).To(HaveKey("config"))
			config := connector["config"].(map[string]interface{})
			Expect(config).To(HaveKey("issuer"))
			Expect(config["issuer"]).To(Equal("https://kubernetes.default.svc"))
			Expect(config).To(HaveKey("clientID"))
			Expect(config["clientID"]).To(Equal("system:serviceaccount:default:test-grafana-dex"))
			Expect(config).To(HaveKey("clientSecret"))
			clientSecret := config["clientSecret"].(string)
			Expect(clientSecret).NotTo(BeEmpty())
			// Token review
			tr := &authenticationv1.TokenReview{
				Spec: authenticationv1.TokenReviewSpec{
					Token: clientSecret,
				},
			}
			By("Checking the client secret is valid")
			Eventually(func(g Gomega) error {
				err := k8sClient.Create(ctx, tr)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}, time.Minute, time.Second).Should(Succeed())
			Expect(tr.Status.Authenticated).To(BeTrue())
			Expect(tr.Status.User.Username).To(Equal("system:serviceaccount:default:test-grafana-dex"))
		})
		It("Should create a deployment and update the sha when the secret is changed", func() {
			firstDeployment := &appsv1.Deployment{}
			By("Getting the deployment")
			deploymentTypeNamespacedName := types.NamespacedName{
				Name:      fmt.Sprintf("%s-dex", resourceName),
				Namespace: "default",
			}
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, deploymentTypeNamespacedName, firstDeployment)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}, time.Minute, time.Second).Should(Succeed())
			By("checking the sha")
			Expect(firstDeployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(firstDeployment.Spec.Template.ObjectMeta.Annotations).To(HaveKey("checksum/config.yaml"))
			firstSha := firstDeployment.Spec.Template.ObjectMeta.Annotations["checksum/config.yaml"]
			Expect(firstSha).NotTo(BeEmpty())
			By("Deleting the secret")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-dex-token", resourceName),
					Namespace: "default",
				},
			}
			secretNamespacedName := types.NamespacedName{
				Name:      fmt.Sprintf("%s-dex-token", resourceName),
				Namespace: "default",
			}
			Eventually(func(g Gomega) error {
				err := k8sClient.Delete(ctx, secret)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}, time.Minute, time.Second).Should(Succeed())
			By("Waiting for the secret to be recreated")
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, secretNamespacedName, secret)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}, time.Minute, time.Second).Should(Succeed())
			By("Getting the deployment again")
			secondDeployment := &appsv1.Deployment{}
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, deploymentTypeNamespacedName, secondDeployment)
				g.Expect(err).NotTo(HaveOccurred())
				secondSha := secondDeployment.Spec.Template.ObjectMeta.Annotations["checksum/config.yaml"]
				g.Expect(firstSha).NotTo(Equal(secondSha))
				g.Expect(secondSha).NotTo(BeEmpty())
				return nil
			}, time.Second*15).Should(Succeed())
		})
	})

	Context("When Dex service account is missing", func() {
		It("Should recreate the service account", func() {
			By("Deleting the Dex service account")
			serviceAccount := &corev1.ServiceAccount{}
			serviceAccountTypeNamespacedName := types.NamespacedName{
				Name:      fmt.Sprintf("%s-dex", resourceName),
				Namespace: "default",
			}
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, serviceAccountTypeNamespacedName, serviceAccount)
				g.Expect(err).NotTo(HaveOccurred())
				return k8sClient.Delete(ctx, serviceAccount)
			}, time.Minute, time.Second).Should(Succeed())

			By("Ensuring the service account is recreated")
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, serviceAccountTypeNamespacedName, serviceAccount)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}, time.Minute, time.Second).Should(Succeed())
		})
	})

	Context("When Dex ingress is missing", func() {
		It("Should recreate the ingress", func() {
			By("Deleting the Dex ingress")
			ingress := &networkingv1.Ingress{}
			ingressTypeNamespacedName := types.NamespacedName{
				Name:      fmt.Sprintf("%s-dex", resourceName),
				Namespace: "default",
			}
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, ingressTypeNamespacedName, ingress)
				g.Expect(err).NotTo(HaveOccurred())
				return k8sClient.Delete(ctx, ingress)
			}, time.Minute, time.Second).Should(Succeed())

			By("Ensuring the ingress is recreated")
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, ingressTypeNamespacedName, ingress)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}, time.Minute, time.Second).Should(Succeed())
		})
	})
	Context("When Dex is disabled", func() {
		It("Should not create any Dex-related resources", func() {
			By("Disabling Dex in the Grafana spec")
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, typeNamespacedName, grafana)
				g.Expect(err).NotTo(HaveOccurred())
				grafana.Spec.Dex.Enabled = false
				return k8sClient.Update(ctx, grafana)
			}, time.Minute, time.Second).Should(Succeed())

			By("Ensuring no Dex-related resources are created")
			dexResources := []types.NamespacedName{
				{Name: fmt.Sprintf("%s-dex", resourceName), Namespace: "default"},
				{Name: fmt.Sprintf("%s-dex-client-secret", resourceName), Namespace: "default"},
				{Name: fmt.Sprintf("%s-dex-token", resourceName), Namespace: "default"},
			}
			for _, resource := range dexResources {
				secret := &corev1.Secret{}
				Eventually(func(g Gomega) error {
					err := k8sClient.Get(ctx, resource, secret)
					g.Expect(err).To(HaveOccurred())
					return nil
				}, time.Minute, time.Second).Should(Succeed())
			}
		})
	})
})

var _ = Describe("Dex Metrics", func() {
	Context("When checking metrics registration", func() {
		It("should have metrics defined with correct parameters", func() {
			Expect(dexReconcileTotal).NotTo(BeNil())
			Expect(dexReconcileDuration).NotTo(BeNil())
			Expect(dexResourceOperations).NotTo(BeNil())
			Expect(dexTokenRefreshes).NotTo(BeNil())

			// Reset metrics before checking
			dexReconcileTotal.Reset()

			// Add a sample metric with namespace, name, and result labels (all three required)
			dexReconcileTotal.WithLabelValues("test-ns", "test-name", "success").Inc()

			// Verify the metric was recorded
			count := testutil.ToFloat64(dexReconcileTotal.WithLabelValues("test-ns", "test-name", "success"))
			Expect(count).To(Equal(1.0))
		})
	})
})
