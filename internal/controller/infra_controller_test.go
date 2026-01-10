/*
Copyright 2026.

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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hostedclusterv1alpha1 "github.com/cldmnky/oooi/api/v1alpha1"
)

var _ = Describe("Infra Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		infra := &hostedclusterv1alpha1.Infra{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Infra")
			err := k8sClient.Get(ctx, typeNamespacedName, infra)
			if err != nil && errors.IsNotFound(err) {
				resource := &hostedclusterv1alpha1.Infra{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: hostedclusterv1alpha1.InfraSpec{
						NetworkConfig: hostedclusterv1alpha1.NetworkConfig{
							CIDR:                        "192.168.100.0/24",
							Gateway:                     "192.168.100.1",
							NetworkAttachmentDefinition: "tenant-vlan-100",
							DNSServers: []string{
								"8.8.8.8",
								"8.8.4.4",
							},
						},
						InfraComponents: hostedclusterv1alpha1.InfraComponents{
							DHCP: hostedclusterv1alpha1.DHCPConfig{
								Enabled:    true,
								ServerIP:   "192.168.100.2",
								RangeStart: "192.168.100.10",
								RangeEnd:   "192.168.100.100",
								LeaseTime:  "1h",
							},
							DNS: hostedclusterv1alpha1.DNSConfig{
								Enabled:     true,
								ServerIP:    "192.168.100.3",
								BaseDomain:  "example.com",
								ClusterName: "test-cluster",
							},
							Proxy: hostedclusterv1alpha1.ProxyConfig{
								Enabled:  true,
								ServerIP: "192.168.100.10",
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &hostedclusterv1alpha1.Infra{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance Infra")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &InfraReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create a DHCPServer CR when DHCP is enabled", func() {
			By("Reconciling the Infra resource")
			controllerReconciler := &InfraReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the DHCPServer CR was created")
			dhcpServer := &hostedclusterv1alpha1.DHCPServer{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      resourceName + "-dhcp",
				Namespace: "default",
			}, dhcpServer)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying DHCPServer has correct configuration")
			Expect(dhcpServer.Spec.NetworkConfig.CIDR).To(Equal("192.168.100.0/24"))
			Expect(dhcpServer.Spec.NetworkConfig.Gateway).To(Equal("192.168.100.1"))
			Expect(dhcpServer.Spec.NetworkConfig.ServerIP).To(Equal("192.168.100.2"))
			Expect(dhcpServer.Spec.LeaseConfig.RangeStart).To(Equal("192.168.100.10"))
			Expect(dhcpServer.Spec.LeaseConfig.RangeEnd).To(Equal("192.168.100.100"))

			By("Verifying owner reference is set")
			Expect(dhcpServer.OwnerReferences).To(HaveLen(1))
			Expect(dhcpServer.OwnerReferences[0].Name).To(Equal(resourceName))
			Expect(dhcpServer.OwnerReferences[0].Kind).To(Equal("Infra"))
		})

		It("should update Infra status when reconciliation succeeds", func() {
			By("Reconciling the Infra resource")
			controllerReconciler := &InfraReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Fetching the updated Infra resource")
			updatedInfra := &hostedclusterv1alpha1.Infra{}
			err = k8sClient.Get(ctx, typeNamespacedName, updatedInfra)
			Expect(err).NotTo(HaveOccurred())

			By("Verifying status conditions are set")
			Expect(updatedInfra.Status.Conditions).NotTo(BeEmpty())
			Expect(updatedInfra.Status.ComponentStatus.DHCPReady).To(BeTrue())
		})
	})
})
