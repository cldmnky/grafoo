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

	grafanav1beta1 "github.com/grafana/grafana-operator/v5/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
					// TODO(user): Specify other spec details if needed.
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
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &GrafanaReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
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
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
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
	})
})
