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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	hostedclusterv1alpha1 "github.com/cldmnky/oooi/api/v1alpha1"
)

var _ = Describe("DHCPServer Controller", func() {
	Context("When reconciling a DHCPServer resource", func() {
		const resourceName = "test-dhcpserver"
		const resourceNamespace = "default"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: resourceNamespace,
		}

		BeforeEach(func() {
			By("creating the custom resource for the Kind DHCPServer")
			dhcpServer := &hostedclusterv1alpha1.DHCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: resourceNamespace,
				},
				Spec: hostedclusterv1alpha1.DHCPServerSpec{
					NetworkConfig: hostedclusterv1alpha1.DHCPNetworkConfig{
						CIDR:     "192.168.100.0/24",
						Gateway:  "192.168.100.1",
						ServerIP: "192.168.100.2",
						DNSServers: []string{
							"8.8.8.8",
							"8.8.4.4",
						},
					},
					LeaseConfig: hostedclusterv1alpha1.DHCPLeaseConfig{
						RangeStart: "192.168.100.10",
						RangeEnd:   "192.168.100.100",
						LeaseTime:  "1h",
					},
					Image: "ghcr.io/cldmnky/hyperdhcp:latest",
				},
			}
			Expect(k8sClient.Create(ctx, dhcpServer)).To(Succeed())
		})

		AfterEach(func() {
			By("cleaning up the DHCPServer resource")
			resource := &hostedclusterv1alpha1.DHCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err != nil && errors.IsNotFound(err) {
				// Resource already deleted, skip cleanup
				return
			}
			Expect(err).NotTo(HaveOccurred())

			By("deleting the DHCPServer resource")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should successfully reconcile the resource", func() {
			By("reconciling the created resource")
			controllerReconciler := &DHCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create a Deployment for the DHCP server", func() {
			By("reconciling the DHCPServer resource")
			controllerReconciler := &DHCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying the Deployment was created")
			deployment := &appsv1.Deployment{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      resourceName + "-dhcp",
				Namespace: resourceNamespace,
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the Deployment has correct image")
			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal("ghcr.io/cldmnky/hyperdhcp:latest"))

			By("verifying owner reference is set")
			Expect(deployment.OwnerReferences).To(HaveLen(1))
			Expect(deployment.OwnerReferences[0].Name).To(Equal(resourceName))
			Expect(deployment.OwnerReferences[0].Kind).To(Equal("DHCPServer"))
		})

		It("should create a ConfigMap with DHCP configuration", func() {
			By("reconciling the DHCPServer resource")
			controllerReconciler := &DHCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying the ConfigMap was created")
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      resourceName + "-dhcp-config",
				Namespace: resourceNamespace,
			}, configMap)
			Expect(err).NotTo(HaveOccurred())

			By("verifying ConfigMap contains DHCP configuration")
			Expect(configMap.Data).To(HaveKey("hyperdhcp.yaml"))
			Expect(configMap.Data["hyperdhcp.yaml"]).To(ContainSubstring("server4:"))
			Expect(configMap.Data["hyperdhcp.yaml"]).To(ContainSubstring("listen:"))
			Expect(configMap.Data["hyperdhcp.yaml"]).To(ContainSubstring("%net1"))
			Expect(configMap.Data["hyperdhcp.yaml"]).To(ContainSubstring("plugins:"))
			Expect(configMap.Data["hyperdhcp.yaml"]).To(ContainSubstring("kubevirt:"))
			Expect(configMap.Data["hyperdhcp.yaml"]).To(ContainSubstring("server_id: 192.168.100.2"))
			Expect(configMap.Data["hyperdhcp.yaml"]).To(ContainSubstring("range: /var/lib/dhcp/leases.txt 192.168.100.10 192.168.100.100"))

			By("verifying owner reference is set")
			Expect(configMap.OwnerReferences).To(HaveLen(1))
			Expect(configMap.OwnerReferences[0].Name).To(Equal(resourceName))
		})

		It("should update status conditions when reconciliation succeeds", func() {
			By("reconciling the DHCPServer resource")
			controllerReconciler := &DHCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("fetching the updated DHCPServer resource")
			updatedDHCPServer := &hostedclusterv1alpha1.DHCPServer{}
			err = k8sClient.Get(ctx, typeNamespacedName, updatedDHCPServer)
			Expect(err).NotTo(HaveOccurred())

			By("verifying status conditions are set")
			Expect(updatedDHCPServer.Status.Conditions).NotTo(BeEmpty())
		})

		It("should handle DHCPServer deletion gracefully", func() {
			By("deleting the DHCPServer resource")
			dhcpServer := &hostedclusterv1alpha1.DHCPServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, dhcpServer)
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Delete(ctx, dhcpServer)).To(Succeed())

			By("reconciling after deletion")
			controllerReconciler := &DHCPServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying the resource is gone")
			err = k8sClient.Get(ctx, typeNamespacedName, dhcpServer)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})
})
