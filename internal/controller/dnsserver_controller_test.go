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

var _ = Describe("DNSServer Controller", func() {
	Context("When reconciling a DNSServer resource", func() {
		const resourceName = "test-dnsserver"
		const resourceNamespace = "default"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: resourceNamespace,
		}

		BeforeEach(func() {
			By("creating the custom resource for the Kind DNSServer")
			dnsServer := &hostedclusterv1alpha1.DNSServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: resourceNamespace,
				},
				Spec: hostedclusterv1alpha1.DNSServerSpec{
					NetworkConfig: hostedclusterv1alpha1.DNSNetworkConfig{
						ServerIP: "192.168.100.3",
						ProxyIP:  "192.168.100.10",
						DNSPort:  53,
					},
					HostedClusterDomain: "my-cluster.example.com",
					StaticEntries: []hostedclusterv1alpha1.DNSStaticEntry{
						{
							Hostname: "api.my-cluster.example.com",
							IP:       "192.168.100.10",
						},
						{
							Hostname: "api-int.my-cluster.example.com",
							IP:       "192.168.100.10",
						},
					},
					UpstreamDNS: []string{
						"8.8.8.8",
						"8.8.4.4",
					},
					Image:          "quay.io/cldmnky/oooi:latest",
					ReloadInterval: "5s",
					CacheTTL:       "30s",
				},
			}
			Expect(k8sClient.Create(ctx, dnsServer)).To(Succeed())
		})

		AfterEach(func() {
			By("cleaning up the DNSServer resource")
			resource := &hostedclusterv1alpha1.DNSServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err != nil && errors.IsNotFound(err) {
				// Resource already deleted, skip cleanup
				return
			}
			Expect(err).NotTo(HaveOccurred())

			By("deleting the DNSServer resource")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should successfully reconcile the resource", func() {
			By("reconciling the created resource")
			controllerReconciler := &DNSServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create a Deployment for the DNS server", func() {
			By("reconciling the DNSServer resource")
			controllerReconciler := &DNSServerReconciler{
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
				Name:      resourceName + "-dns",
				Namespace: resourceNamespace,
			}, deployment)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the Deployment has correct image")
			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal("quay.io/cldmnky/oooi:latest"))

			By("verifying the Deployment runs dns subcommand")
			Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElement("dns"))

			By("verifying owner reference is set")
			Expect(deployment.OwnerReferences).To(HaveLen(1))
			Expect(deployment.OwnerReferences[0].Name).To(Equal(resourceName))
			Expect(deployment.OwnerReferences[0].Kind).To(Equal("DNSServer"))
		})

		It("should create a ConfigMap with Corefile configuration", func() {
			By("reconciling the DNSServer resource")
			controllerReconciler := &DNSServerReconciler{
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
				Name:      resourceName + "-dns-config",
				Namespace: resourceNamespace,
			}, configMap)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the ConfigMap contains Corefile")
			Expect(configMap.Data).To(HaveKey("Corefile"))
			corefile := configMap.Data["Corefile"]

			By("verifying the Corefile contains static entries")
			Expect(corefile).To(ContainSubstring("api.my-cluster.example.com"))
			Expect(corefile).To(ContainSubstring("api-int.my-cluster.example.com"))
			Expect(corefile).To(ContainSubstring("192.168.100.10"))

			By("verifying the Corefile has dual views")
			Expect(corefile).To(ContainSubstring("VIEW 1: Multus secondary network"))
			Expect(corefile).To(ContainSubstring("VIEW 2: Pod network interface"))
			Expect(corefile).To(ContainSubstring("bind 192.168.100.3"))

			By("verifying the Corefile contains upstream DNS")
			Expect(corefile).To(ContainSubstring("8.8.8.8"))

			By("verifying the Corefile contains reload interval")
			Expect(corefile).To(ContainSubstring("reload 5s"))

			By("verifying owner reference is set")
			Expect(configMap.OwnerReferences).To(HaveLen(1))
			Expect(configMap.OwnerReferences[0].Name).To(Equal(resourceName))
			Expect(configMap.OwnerReferences[0].Kind).To(Equal("DNSServer"))
		})

		It("should create a Service for the DNS server", func() {
			By("reconciling the DNSServer resource")
			controllerReconciler := &DNSServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying the Service was created")
			service := &corev1.Service{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      resourceName + "-dns",
				Namespace: resourceNamespace,
			}, service)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the Service exposes DNS port")
			Expect(service.Spec.Ports).To(HaveLen(2))
			// Find UDP port
			var udpPort *corev1.ServicePort
			for i := range service.Spec.Ports {
				if service.Spec.Ports[i].Protocol == corev1.ProtocolUDP {
					udpPort = &service.Spec.Ports[i]
					break
				}
			}
			Expect(udpPort).NotTo(BeNil())
			Expect(udpPort.Port).To(Equal(int32(53)))

			By("verifying owner reference is set")
			Expect(service.OwnerReferences).To(HaveLen(1))
			Expect(service.OwnerReferences[0].Name).To(Equal(resourceName))
			Expect(service.OwnerReferences[0].Kind).To(Equal("DNSServer"))
		})

		It("should update status with Ready condition", func() {
			By("reconciling the DNSServer resource")
			controllerReconciler := &DNSServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("fetching the updated DNSServer resource")
			dnsServer := &hostedclusterv1alpha1.DNSServer{}
			err = k8sClient.Get(ctx, typeNamespacedName, dnsServer)
			Expect(err).NotTo(HaveOccurred())

			By("verifying status fields are populated")
			Expect(dnsServer.Status.ConfigMapName).To(Equal(resourceName + "-dns-config"))
			Expect(dnsServer.Status.DeploymentName).To(Equal(resourceName + "-dns"))
			Expect(dnsServer.Status.ObservedGeneration).To(Equal(dnsServer.Generation))

			By("verifying Ready condition is present")
			Expect(dnsServer.Status.Conditions).NotTo(BeEmpty())
			readyCondition := findCondition(dnsServer.Status.Conditions, "Ready")
			Expect(readyCondition).NotTo(BeNil())
			Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
		})
	})
})

// Helper function to find a condition by type
func findCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}
