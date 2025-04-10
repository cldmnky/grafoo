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
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	grafoov1alpha1 "github.com/cldmnky/grafoo/api/v1alpha1"
)

var _ = Describe("Grafana Controller", func() {

	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
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
	/*
		Context("When reconciling a resource with status conditions", func() {
			It("should update token times when refresh is needed", func(ctx SpecContext) {
				// Update the status
				Eventually(func() error {
					err := k8sClient.Get(ctx, typeNamespacedName, grafana)
					Expect(err).NotTo(HaveOccurred())
					grafana.Status.TokenExpirationTime = &metav1.Time{
						Time: time.Now().Add(-time.Hour),
					}
					grafana.Status.Conditions = []metav1.Condition{
						{
							Type:               typeAvailable,
							Status:             metav1.ConditionTrue,
							Reason:             "Available",
							Message:            "Grafana is available",
							LastTransitionTime: metav1.Now(),
						},
					}
					err = k8sClient.Status().Update(ctx, grafana)
					if err != nil {
						Expect(err).NotTo(HaveOccurred())
					}
					// Get the resource again
					err = k8sClient.Get(ctx, typeNamespacedName, grafana)
					Expect(err).NotTo(HaveOccurred())
					return nil
				}, time.Minute, time.Second).Should(Succeed())
				// Check if the token expiration time is updated
				// This is a simplified check, in a real scenario you would check the actual token
				// generation logic
				// Here we just check if the token expiration time is in the future
				// and the token generation time is set

				// Updated token should be in the future
				if grafana.Status.TokenExpirationTime != nil {
					// Sleep 10 seconds to ensure the token is updated
					Eventually(func() error {
						err := k8sClient.Get(ctx, typeNamespacedName, grafana)
						Expect(err).NotTo(HaveOccurred())
						// Check if the token expiration time is in the future
						if grafana.Status.TokenExpirationTime.Time.Before(time.Now()) {
							return fmt.Errorf("token expiration time is not in the future")
						}
						return nil
					}, time.Minute, time.Second).Should(Succeed())
				}

				// Verify token generation time is set
				Expect(grafana.Status.TokenGenerationTime).NotTo(BeNil())

				// Verify expiration time is set to spec duration from now
				expectedExpiration := grafana.Status.TokenGenerationTime.Add(grafana.Spec.TokenDuration.Duration)
				Expect(grafana.Status.TokenExpirationTime.Time).To(BeTemporally("~", expectedExpiration, time.Minute))
			})

			It("should initialize status conditions when they are empty", func(ctx SpecContext) {
				// Create a Grafana resource with no status conditions
				grafana := &grafoov1alpha1.Grafana{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-conditions",
						Namespace: "default",
					},
					Spec: grafoov1alpha1.GrafanaSpec{},
				}

				// Create the resource first
				Expect(k8sClient.Create(ctx, grafana)).To(Succeed())
				defer k8sClient.Delete(ctx, grafana)

				// Wait for status conditions to be initialized
				Eventually(func() int {
					err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-conditions", Namespace: "default"}, grafana)
					if err != nil {
						return 0
					}
					return len(grafana.Status.Conditions)
				}, time.Minute, time.Second).Should(Equal(5))

				// Verify all condition types are present
				conditionTypes := []string{typeAvailable, typeDexReady, typeMariaDBReady, typeDataSourcesReady, typeGrafanaReady}
				for _, condType := range conditionTypes {
					var found bool
					for _, cond := range grafana.Status.Conditions {
						if cond.Type == condType {
							found = true
							break
						}
					}
					Expect(found).To(BeTrue(), "Condition %s should be present", condType)
				}
			})
		})
	*/
	Context("When reconciling a resource", func() {
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
				g.Expect(grafana.Status.Conditions).To(HaveLen(5))
				g.Expect(grafana.Status.TokenGenerationTime).ToNot(BeNil())
				return nil
			}, time.Minute, time.Second).Should(Succeed())
			Expect(grafana.Status).NotTo(BeNil())
			// Check the conditions
			Expect(grafana.Status.Conditions[0].Type).To(Equal(typeAvailable))
			Expect(grafana.Status.Conditions[0].Status).To(Equal(metav1.ConditionTrue))
			// Check token expiration time
			Expect(grafana.Status.TokenExpirationTime).NotTo(BeNil())
			Expect(grafana.Status.TokenExpirationTime.Time).To(BeTemporally("~", time.Now().Add(grafana.Spec.TokenDuration.Duration), time.Minute))
			Expect(grafana.Status.TokenGenerationTime.Time).To(BeTemporally("<", time.Now(), time.Second))
		})
	})

})
