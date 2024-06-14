/*
Copyright 2024 Magnus Bengtsson <magnus@cloudmonkey.org>.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"time"

	grafanav1beta1 "github.com/grafana/grafana-operator/v5/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
)

var _ = Describe("Grafana Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		grafana := &grafoov1alpha1.Grafana{}
		grafanaOperated := &grafanav1beta1.Grafana{}

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
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      resourceName + "-sa",
				Namespace: "default",
			}, serviceAccount)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Delete(ctx, serviceAccount)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &GrafanaReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Clientset: clientSet,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, typeNamespacedName, grafana)).To(Succeed())
			// expect a grafana instance to be created
			Expect(k8sClient.Get(ctx, typeNamespacedName, grafanaOperated)).To(Succeed())
			// The Grafana instance should have the same name as the custom resource
			Expect(grafanaOperated.Name).To(Equal(resourceName))
			// The grafana instance should have owner reference set to the custom resource
			Expect(grafanaOperated.OwnerReferences).To(HaveLen(1))
			Expect(grafanaOperated.OwnerReferences[0].Name).To(Equal(resourceName))
		})
		// Dex is enabled
		It("should successfully reconcile the resource with Dex enabled", func() {
			By("Reconciling the created resource")
			// get the resource
			Expect(k8sClient.Get(ctx, typeNamespacedName, grafana)).To(Succeed())
			grafana.Spec.Dex = &grafoov1alpha1.Dex{
				Enabled: true,
			}
			Expect(k8sClient.Update(ctx, grafana)).To(Succeed())

			controllerReconciler := &GrafanaReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Clientset: clientSet,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, typeNamespacedName, grafana)).To(Succeed())
			// expect a grafana instance to be created
			Expect(k8sClient.Get(ctx, typeNamespacedName, grafanaOperated)).To(Succeed())
			// The Grafana instance should have the same name as the custom resource
			Expect(grafanaOperated.Name).To(Equal(resourceName))
			// The grafana instance should have owner reference set to the custom resource
			Expect(grafanaOperated.OwnerReferences).To(HaveLen(1))
			Expect(grafanaOperated.OwnerReferences[0].Name).To(Equal(resourceName))
		})
		It("should successfully reconcile the resource with Dex disabled", func() {
			By("Reconciling the created resource")
			// get the resource
			Expect(k8sClient.Get(ctx, typeNamespacedName, grafana)).To(Succeed())
			grafana.Spec.Dex = &grafoov1alpha1.Dex{
				Enabled: false,
			}
			Expect(k8sClient.Update(ctx, grafana)).To(Succeed())

			controllerReconciler := &GrafanaReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Clientset: clientSet,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, typeNamespacedName, grafana)).To(Succeed())
			// expect a grafana instance to be created
			Expect(k8sClient.Get(ctx, typeNamespacedName, grafanaOperated)).To(Succeed())
			// The Grafana instance should have the same name as the custom resource
			Expect(grafanaOperated.Name).To(Equal(resourceName))
			// The grafana instance should have owner reference set to the custom resource
			Expect(grafanaOperated.OwnerReferences).To(HaveLen(1))
			Expect(grafanaOperated.OwnerReferences[0].Name).To(Equal(resourceName))
		})
		// client secret
		It("should successfully reconcile the resource with Dex enabled and client secret", func() {
			By("Reconciling the created resource")
			// get the resource
			Expect(k8sClient.Get(ctx, typeNamespacedName, grafana)).To(Succeed())
			grafana.Spec.Dex = &grafoov1alpha1.Dex{
				Enabled: true,
			}
			Expect(k8sClient.Update(ctx, grafana)).To(Succeed())

			controllerReconciler := &GrafanaReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Clientset: clientSet,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, typeNamespacedName, grafana)).To(Succeed())
			// expect a grafana instance to be created
			Expect(k8sClient.Get(ctx, typeNamespacedName, grafanaOperated)).To(Succeed())
			// The Grafana instance should have the same name as the custom resource
			Expect(grafanaOperated.Name).To(Equal(resourceName))
			// The grafana instance should have owner reference set to the custom resource
			Expect(grafanaOperated.OwnerReferences).To(HaveLen(1))
			Expect(grafanaOperated.OwnerReferences[0].Name).To(Equal(resourceName))
			By("Checking the client secret")
			// check the client secret
			clientSecret := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      grafanaOperated.Name + "-dex-client-secret",
				Namespace: grafanaOperated.Namespace,
			}, clientSecret)
			Expect(err).NotTo(HaveOccurred())
			clientSecretValue := clientSecret.Data["clientSecret"]
			Expect(clientSecretValue).NotTo(BeNil())
			Expect(clientSecretValue).NotTo(BeEmpty())
			By("Reconciling the created resource again")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// Get the client secret again
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      grafanaOperated.Name + "-dex-client-secret",
				Namespace: grafanaOperated.Namespace,
			}, clientSecret)
			Expect(err).NotTo(HaveOccurred())
			clientSecretValueAgain := clientSecret.Data["clientSecret"]
			Expect(clientSecretValueAgain).NotTo(BeNil())
			Expect(clientSecretValueAgain).NotTo(BeEmpty())
			Expect(clientSecretValueAgain).To(Equal(clientSecretValue))

			By("Checking that the dex config secret has the same client secret")
			dexConfigSecret := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      grafanaOperated.Name + "-dex",
				Namespace: grafanaOperated.Namespace,
			}, dexConfigSecret)
			Expect(err).NotTo(HaveOccurred())
			config := dexConfigSecret.Data["config.yaml"]
			Expect(config).NotTo(BeNil())
			Expect(config).NotTo(BeEmpty())
			Expect(string(config)).To(ContainSubstring(string(clientSecretValue)))
		})
		// Clusterrolebinding
		It("should successfully create a cluster role binding for the grafana account", func() {
			By("Reconciling the created resource")
			// get the resource
			Expect(k8sClient.Get(ctx, typeNamespacedName, grafana)).To(Succeed())
			Expect(k8sClient.Update(ctx, grafana)).To(Succeed())

			controllerReconciler := &GrafanaReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Clientset: clientSet,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, typeNamespacedName, grafana)).To(Succeed())
			// expect a grafana instance to be created
			Expect(k8sClient.Get(ctx, typeNamespacedName, grafanaOperated)).To(Succeed())
			// The Grafana instance should have the same name as the custom resource
			Expect(grafanaOperated.Name).To(Equal(resourceName))
			// The grafana instance should have owner reference set to the custom resource
			Expect(grafanaOperated.OwnerReferences).To(HaveLen(1))
			Expect(grafanaOperated.OwnerReferences[0].Name).To(Equal(resourceName))
			By("Checking the cluster role binding")
			clusterRoleBinding := &rbacv1.ClusterRoleBinding{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: grafanaOperated.Name + "-cluster-monitoring-view",
			}, clusterRoleBinding)
			Expect(err).NotTo(HaveOccurred())
			Expect(clusterRoleBinding.Subjects).To(HaveLen(1))
			Expect(clusterRoleBinding.Subjects[0].Name).To(Equal(grafanaOperated.Name + "-sa"))
			Expect(clusterRoleBinding.Subjects[0].Kind).To(Equal("ServiceAccount"))
			Expect(clusterRoleBinding.Subjects[0].Namespace).To(Equal(grafanaOperated.Namespace))
			// The cluster role binding should have the correct role
			Expect(clusterRoleBinding.RoleRef.Name).To(Equal("cluster-monitoring-view"))
			Expect(clusterRoleBinding.RoleRef.Kind).To(Equal("ClusterRole"))
			Expect(clusterRoleBinding.RoleRef.APIGroup).To(Equal("rbac.authorization.k8s.io"))
		})
		// status
		It("should successfully update the status of the resource", func() {
			By("Reconciling the created resource")
			// get the resource
			Expect(k8sClient.Get(ctx, typeNamespacedName, grafana)).To(Succeed())
			Expect(k8sClient.Update(ctx, grafana)).To(Succeed())

			controllerReconciler := &GrafanaReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Clientset: clientSet,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, typeNamespacedName, grafana)).To(Succeed())
			Expect(grafana.Status).NotTo(BeNil())
			Expect(grafana.Status.TokenExpirationTime).NotTo(BeNil())
			Expect(grafana.Status.TokenExpirationTime.Time).NotTo(BeNil())
			Expect(grafana.Status.TokenExpirationTime.Time).NotTo(BeZero())

		})
	})
})
