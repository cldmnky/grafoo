package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
)

var _ = Describe("MariaDB Controller", func() {
	const resourceName = "test-grafana"

	ctx := context.Background()

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
	Context("When reconciling a Grafana with MariaDB disabled", func() {
		It("Should not create a MariaDB deployment", func() {
			By("Disabling MariaDB")
			// get the resource
			Expect(k8sClient.Get(ctx, typeNamespacedName, grafana)).To(Succeed())
			grafana.Spec.MariaDB.Enabled = false
			Expect(k8sClient.Update(ctx, grafana)).To(Succeed())
			// Get the resource again
			Expect(k8sClient.Get(ctx, typeNamespacedName, grafana)).To(Succeed())
			Expect(grafana.Spec.MariaDB.Enabled).To(BeFalse())

			By("Reconciling the created resource")
			controllerReconciler := &GrafanaReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Clientset: clientSet,
				Dynamic:   dynamicClient,
			}
			_, err := controllerReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Checking if the MariaDB deployment exists")
			mariadbDeployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "test-grafana-mariadb", Namespace: "default"}, mariadbDeployment)
			Expect(err).To(HaveOccurred())
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})

	Context("When reconciling a Grafana with MariaDB enabled", func() {
		BeforeEach(func() {
			By("Enabling MariaDB")
			// get the resource
			Expect(k8sClient.Get(ctx, typeNamespacedName, grafana)).To(Succeed())

			grafana.Spec.MariaDB.Enabled = true
			Expect(k8sClient.Update(ctx, grafana)).To(Succeed())
		})
		It("Should create a MariaDB secret once", func() {
			By("Reconciling the created resource")
			controllerReconciler := &GrafanaReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Clientset: clientSet,
				Dynamic:   dynamicClient,
			}
			_, err := controllerReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			By("Getting the MariaDB secret")
			mariadbSecret := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "test-grafana-mariadb", Namespace: "default"}, mariadbSecret)
			Expect(err).NotTo(HaveOccurred())
			Expect(mariadbSecret.Data).To(HaveKey("database-name"))
			Expect(mariadbSecret.Data).To(HaveKey("database-password"))
			Expect(mariadbSecret.Data).To(HaveKey("database-root-password"))
			Expect(mariadbSecret.Data).To(HaveKey("database-user"))
			//Get the passwords
			password := mariadbSecret.Data["database-password"]
			rootPassword := mariadbSecret.Data["database-root-password"]
			Expect(password).ToNot(BeEmpty())
			Expect(rootPassword).ToNot(BeEmpty())
			By("Reconciling the created resource again")
			_, err = controllerReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			//Get the secret again
			mariadbSecret = &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "test-grafana-mariadb", Namespace: "default"}, mariadbSecret)
			Expect(err).NotTo(HaveOccurred())
			By("Checking if the MariaDB secret has the same passwords")
			Expect(mariadbSecret.Data["database-password"]).To(Equal(password))
			Expect(mariadbSecret.Data["database-root-password"]).To(Equal(rootPassword))
		})
		It("Should create a MariaDB service account", func() {
			By("Reconciling the created resource")
			controllerReconciler := &GrafanaReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Clientset: clientSet,
				Dynamic:   dynamicClient,
			}
			_, err := controllerReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			By("Getting the MariaDB service account")
			mariadbServiceAccount := &corev1.ServiceAccount{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "test-grafana-mariadb", Namespace: "default"}, mariadbServiceAccount)
			Expect(err).NotTo(HaveOccurred())
			Expect(mariadbServiceAccount.Labels).To(HaveKeyWithValue("app.kubernetes.io/name", "grafana"))
			Expect(mariadbServiceAccount.Labels).To(HaveKeyWithValue("app.kubernetes.io/component", "mariadb"))
		})
		It("Should create a MariaDB PVC", func() {
			By("Reconciling the created resource")
			controllerReconciler := &GrafanaReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Clientset: clientSet,
				Dynamic:   dynamicClient,
			}
			_, err := controllerReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			By("Getting the MariaDB PVC")
			mariadbPVC := &corev1.PersistentVolumeClaim{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "test-grafana-mariadb", Namespace: "default"}, mariadbPVC)
			Expect(err).NotTo(HaveOccurred())
			Expect(mariadbPVC.Spec.Resources.Requests).To(HaveKeyWithValue(corev1.ResourceStorage, resource.MustParse("1Gi")))
		})
		It("Should create a MariaDB service", func() {
			By("Reconciling the created resource")
			controllerReconciler := &GrafanaReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Clientset: clientSet,
				Dynamic:   dynamicClient,
			}
			_, err := controllerReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			By("Getting the MariaDB service")
			mariadbService := &corev1.Service{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "test-grafana-mariadb", Namespace: "default"}, mariadbService)
			Expect(err).NotTo(HaveOccurred())
			Expect(mariadbService.Labels).To(HaveKeyWithValue("app.kubernetes.io/name", "grafana"))
			Expect(mariadbService.Labels).To(HaveKeyWithValue("app.kubernetes.io/component", "mariadb"))
			// Port
			Expect(mariadbService.Spec.Ports).To(HaveLen(1))
			Expect(mariadbService.Spec.Ports[0].Name).To(Equal("mysql"))
			Expect(mariadbService.Spec.Ports[0].Port).To(Equal(int32(3306)))
		})
		It("Should create a MariaDB deployment", func() {
			By("Reconciling the created resource")
			controllerReconciler := &GrafanaReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Clientset: clientSet,
				Dynamic:   dynamicClient,
			}
			_, err := controllerReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			By("Getting the MariaDB deployment")
			mariadbDeployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "test-grafana-mariadb", Namespace: "default"}, mariadbDeployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(mariadbDeployment.Labels).To(HaveKeyWithValue("app.kubernetes.io/name", "grafana"))
			Expect(mariadbDeployment.Labels).To(HaveKeyWithValue("app.kubernetes.io/component", "mariadb"))
			// Pod spec
			Expect(mariadbDeployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(mariadbDeployment.Spec.Template.Spec.Containers[0].Name).To(Equal("mariadb"))
			Expect(mariadbDeployment.Spec.Template.Spec.Containers[0].Image).To(Equal(grafoov1alpha1.MariaDBImage))

			// Volume
			Expect(mariadbDeployment.Spec.Template.Spec.Volumes).To(HaveLen(2))
			Expect(mariadbDeployment.Spec.Template.Spec.Volumes[0].Name).To(Equal("mariadb-data"))
			Expect(mariadbDeployment.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName).To(Equal("test-grafana-mariadb"))
			Expect(mariadbDeployment.Spec.Template.Spec.Volumes[1].Name).To(Equal("kube-api-access"))
			Expect(mariadbDeployment.Spec.Template.Spec.Volumes[1].Projected.Sources).To(HaveLen(4))
			// Port
			Expect(mariadbDeployment.Spec.Template.Spec.Containers[0].Ports).To(HaveLen(1))
			Expect(mariadbDeployment.Spec.Template.Spec.Containers[0].Ports[0].Name).To(Equal("mysql"))
			Expect(mariadbDeployment.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort).To(Equal(int32(3306)))
		})
	})
})
