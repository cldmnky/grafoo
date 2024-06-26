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
	"time"

	grafanav1beta1 "github.com/grafana/grafana-operator/v5/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
)

var _ = Describe("Grafana Controller", func() {
	Context("When reconciling a resource", func() {

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		grafana := &grafoov1alpha1.Grafana{}
		grafanaOperated := &grafanav1beta1.Grafana{}

		BeforeEach(func(ctx SpecContext) {
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
							Image:   grafoov1alpha1.DexImage,
						},
						MariaDB: &grafoov1alpha1.MariaDB{
							Enabled:     true,
							StorageSize: "1Gi",
							Image:       grafoov1alpha1.MariaDBImage,
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func(ctx SpecContext) {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &grafoov1alpha1.Grafana{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance Grafana")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

		})
		It("should successfully reconcile the resource", func() {
			Expect(k8sClient.Get(ctx, typeNamespacedName, grafana)).To(Succeed())
			// expect a grafana instance to be created
			Eventually(func() error {
				return k8sClient.Get(ctx, typeNamespacedName, grafanaOperated)
			}, time.Minute, time.Second).Should(Succeed())
			Expect(k8sClient.Get(ctx, typeNamespacedName, grafanaOperated)).To(Succeed())
			// The Grafana instance should have the same name as the custom resource
			Expect(grafanaOperated.Name).To(Equal(resourceName))
			// The grafana instance should have owner reference set to the custom resource
			Expect(grafanaOperated.OwnerReferences).To(HaveLen(1))
			Expect(grafanaOperated.OwnerReferences[0].Name).To(Equal(resourceName))
			By("Checking the defaults")
			// check the defaults
			// Version
			Expect(grafana.Spec.Version).To(Equal(grafoov1alpha1.GrafanaVersion))
			Expect(grafanaOperated.Spec.Version).To(Equal(grafoov1alpha1.GrafanaVersion))
			// Token duration
			Expect(grafana.Spec.TokenDuration.Duration).To(Equal(grafoov1alpha1.TokenDuration.Duration))
		})
		// Dex is enabled - move to dex test
		It("should successfully reconcile the resource with Dex enabled", func() {
			Eventually(func(g Gomega) error {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, grafana)).To(Succeed())
				grafana.Spec.Dex = &grafoov1alpha1.Dex{
					Enabled: true,
				}
				err := k8sClient.Update(ctx, grafana)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}, time.Minute, time.Second).Should(Succeed())
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
			Eventually(func(g Gomega) error {
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, grafana)).To(Succeed())
				grafana.Spec.Dex = &grafoov1alpha1.Dex{
					Enabled: false,
				}
				err := k8sClient.Update(ctx, grafana)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}, time.Minute, time.Second).Should(Succeed())
			Expect(k8sClient.Get(ctx, typeNamespacedName, grafana)).To(Succeed())
			Expect(k8sClient.Get(ctx, typeNamespacedName, grafanaOperated)).To(Succeed())
			// The Grafana instance should have the same name as the custom resource
			Expect(grafanaOperated.Name).To(Equal(resourceName))
			// The grafana instance should have owner reference set to the custom resource
			Expect(grafanaOperated.OwnerReferences).To(HaveLen(1))
			Expect(grafanaOperated.OwnerReferences[0].Name).To(Equal(resourceName))
		})
		// client secret
		It("should successfully reconcile the resource with Dex enabled and client secret", func() {
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
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      grafanaOperated.Name + "-dex-client-secret",
				Namespace: grafanaOperated.Namespace,
			}, clientSecret)
			Expect(err).NotTo(HaveOccurred())
			clientSecretValue := clientSecret.Data["clientSecret"]
			Expect(clientSecretValue).NotTo(BeNil())
			Expect(clientSecretValue).NotTo(BeEmpty())

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
			// expect a grafana instance to be created
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, typeNamespacedName, grafanaOperated)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}).Should(Succeed())
			// The Grafana instance should have the same name as the custom resource
			Expect(grafanaOperated.Name).To(Equal(resourceName))
			// The grafana instance should have owner reference set to the custom resource
			Expect(grafanaOperated.OwnerReferences).To(HaveLen(1))
			Expect(grafanaOperated.OwnerReferences[0].Name).To(Equal(resourceName))
			By("Checking the cluster role binding")
			clusterRoleBinding := &rbacv1.ClusterRoleBinding{}
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: grafanaOperated.Name + "-cluster-monitoring-view",
				}, clusterRoleBinding)
				g.Expect(err).NotTo(HaveOccurred())
				return nil
			}).Should(Succeed())
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
			// get the resource expect status to be unknown
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, typeNamespacedName, grafana)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(grafana.Status).NotTo(BeNil())
				g.Expect(grafana.Status.Conditions).To(HaveLen(4))
				return nil
			}, time.Minute, time.Second).Should(Succeed())
			Expect(grafana.Status).NotTo(BeNil())
			// Check the conditions
			Expect(grafana.Status.Conditions[0].Type).To(Equal(typeAvailable))
			Expect(grafana.Status.Conditions[0].Status).To(Equal(metav1.ConditionTrue))
		})

	})
})
