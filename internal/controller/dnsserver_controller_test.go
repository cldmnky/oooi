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
	"regexp"
	"strings"

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
						ServerIP:             "192.168.100.3",
						ProxyIP:              "192.168.100.10",
						SecondaryNetworkCIDR: "192.168.100.0/24",
						DNSPort:              53,
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

			By("verifying the Corefile uses view plugin")
			Expect(corefile).To(ContainSubstring("view multus"))
			Expect(corefile).To(ContainSubstring("view default"))
			Expect(corefile).To(ContainSubstring("incidr(client_ip()"))

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

	Context("Split-horizon DNS with internal proxy configured", func() {
		ctx := context.Background()

		It("should create Corefile with dual-view configuration", func() {
			resourceName := "test-split-1"
			resourceNamespace := "test-ns-split-1"

			typeNamespacedName := types.NamespacedName{
				Name:      resourceName,
				Namespace: resourceNamespace,
			}

			By("creating the namespace")
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: resourceNamespace,
				},
			}
			err := k8sClient.Create(ctx, namespace)
			Expect(err).NotTo(HaveOccurred())

			By("creating the DNSServer resource with InternalProxyIP")
			dnsServer := &hostedclusterv1alpha1.DNSServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: resourceNamespace,
				},
				Spec: hostedclusterv1alpha1.DNSServerSpec{
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
						{
							Hostname: "*.apps.my-cluster.example.com",
							IP:       "192.168.100.10",
						},
					},
					NetworkConfig: hostedclusterv1alpha1.DNSNetworkConfig{
						ServerIP:             "192.168.100.3",
						ProxyIP:              "192.168.100.10",   // External proxy IP for VMs
						InternalProxyIP:      "10.96.100.200",    // Internal proxy IP for pods
						SecondaryNetworkCIDR: "192.168.100.0/24", // Secondary network CIDR
						DNSPort:              53,
					},
					UpstreamDNS: []string{"8.8.8.8", "8.8.4.4"},
					Image:       "quay.io/cldmnky/oooi:latest",
				},
			}
			err = k8sClient.Create(ctx, dnsServer)
			Expect(err).NotTo(HaveOccurred())

			By("reconciling the DNSServer resource")
			controllerReconciler := &DNSServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("fetching the ConfigMap")
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      resourceName + "-dns-config",
				Namespace: resourceNamespace,
			}, configMap)
			Expect(err).NotTo(HaveOccurred())

			corefile := configMap.Data["Corefile"]

			By("verifying Corefile contains view plugin structure")
			Expect(corefile).To(ContainSubstring(".:53 {"))
			Expect(corefile).To(ContainSubstring("view multus {"))
			Expect(corefile).To(ContainSubstring("view default {"))

			By("verifying multus view uses correct CIDR expression")
			Expect(corefile).To(ContainSubstring("expr incidr(client_ip(), '192.168.100.0/24')"))

			By("verifying multus view has hosts entries with external proxy IP")
			Expect(corefile).To(MatchRegexp(`view multus[\s\S]*?192\.168\.100\.10\s+api\.my-cluster\.example\.com`))
			Expect(corefile).To(MatchRegexp(`view multus[\s\S]*?192\.168\.100\.10\s+api-int\.my-cluster\.example\.com`))

			By("verifying default view has hosts entries with internal proxy IP")
			Expect(corefile).To(MatchRegexp(`view default[\s\S]*?10\.96\.100\.200\s+api\.my-cluster\.example\.com`))
			Expect(corefile).To(MatchRegexp(`view default[\s\S]*?10\.96\.100\.200\s+api-int\.my-cluster\.example\.com`))

			By("verifying default view has forward plugin")
			Expect(corefile).To(MatchRegexp(`view default[\s\S]*?forward[\s\S]*?8\.8\.8\.8`))

			By("verifying both views exclude each other")
			// Multus view should not have internal proxy IP
			Expect(corefile).To(ContainSubstring("view multus"))
			// Extract just the multus view section to verify it doesn't have internal IP
			multusStart := strings.Index(corefile, "view multus")
			multusEnd := strings.Index(corefile[multusStart:], "view default")
			if multusEnd > 0 {
				multusView := corefile[multusStart : multusStart+multusEnd]
				Expect(multusView).NotTo(ContainSubstring("10.96.100.200"))
			}

			By("cleaning up")
			k8sClient.Delete(ctx, dnsServer)
			k8sClient.Delete(ctx, namespace)
		})

		It("should have separate hosts entries for each view", func() {
			resourceName := "test-split-2"
			resourceNamespace := "test-ns-split-2"

			typeNamespacedName := types.NamespacedName{
				Name:      resourceName,
				Namespace: resourceNamespace,
			}

			By("creating the namespace")
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: resourceNamespace,
				},
			}
			err := k8sClient.Create(ctx, namespace)
			Expect(err).NotTo(HaveOccurred())

			By("creating the DNSServer resource with InternalProxyIP")
			dnsServer := &hostedclusterv1alpha1.DNSServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: resourceNamespace,
				},
				Spec: hostedclusterv1alpha1.DNSServerSpec{
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
						{
							Hostname: "*.apps.my-cluster.example.com",
							IP:       "192.168.100.10",
						},
					},
					NetworkConfig: hostedclusterv1alpha1.DNSNetworkConfig{
						ServerIP:             "192.168.100.3",
						ProxyIP:              "192.168.100.10",   // External proxy IP for VMs
						InternalProxyIP:      "10.96.100.200",    // Internal proxy IP for pods
						SecondaryNetworkCIDR: "192.168.100.0/24", // Secondary network CIDR
						DNSPort:              53,
					},
					UpstreamDNS: []string{"8.8.8.8", "8.8.4.4"},
					Image:       "quay.io/cldmnky/oooi:latest",
				},
			}
			err = k8sClient.Create(ctx, dnsServer)
			Expect(err).NotTo(HaveOccurred())

			By("reconciling the DNSServer resource")
			controllerReconciler := &DNSServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("fetching the ConfigMap")
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      resourceName + "-dns-config",
				Namespace: resourceNamespace,
			}, configMap)
			Expect(err).NotTo(HaveOccurred())

			corefile := configMap.Data["Corefile"]

			By("verifying all endpoints are in multus view with external proxy")
			Expect(corefile).To(MatchRegexp(`view multus[\s\S]*?192\.168\.100\.10\s+api\.my-cluster\.example\.com`))
			Expect(corefile).To(MatchRegexp(`view multus[\s\S]*?192\.168\.100\.10\s+api-int\.my-cluster\.example\.com`))
			Expect(corefile).To(MatchRegexp(`view multus[\s\S]*?192\.168\.100\.10\s+\*\.apps\.my-cluster\.example\.com`))

			By("verifying all endpoints are in default view with internal proxy")
			Expect(corefile).To(MatchRegexp(`view default[\s\S]*?10\.96\.100\.200\s+api\.my-cluster\.example\.com`))
			Expect(corefile).To(MatchRegexp(`view default[\s\S]*?10\.96\.100\.200\s+api-int\.my-cluster\.example\.com`))
			Expect(corefile).To(MatchRegexp(`view default[\s\S]*?10\.96\.100\.200\s+\*\.apps\.my-cluster\.example\.com`))

			By("verifying each view has exactly 3 endpoints")
			// Count occurrences of each IP in the Corefile
			externalIPCount := strings.Count(corefile, "192.168.100.10")
			internalIPCount := strings.Count(corefile, "10.96.100.200")
			Expect(externalIPCount).To(Equal(3), "External proxy IP should appear 3 times (one per endpoint)")
			Expect(internalIPCount).To(Equal(3), "Internal proxy IP should appear 3 times (one per endpoint)")

			By("cleaning up")
			k8sClient.Delete(ctx, dnsServer)
			k8sClient.Delete(ctx, namespace)
		})
	})

	Context("Split-horizon DNS without internal proxy", func() {
		const resourceName = "test-no-internal"
		const resourceNamespace = "clusters-test-no-internal"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: resourceNamespace,
		}

		BeforeEach(func() {
			By("creating the namespace")
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: resourceNamespace,
				},
			}
			// Try to get the namespace first
			err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceNamespace}, namespace)
			if err != nil && errors.IsNotFound(err) {
				// Namespace doesn't exist, create it
				err = k8sClient.Create(ctx, namespace)
				Expect(err).NotTo(HaveOccurred())
			} else if err == nil {
				// Namespace exists, that's okay
			} else {
				// Some other error occurred
				Expect(err).NotTo(HaveOccurred())
			}

			By("creating the DNSServer resource without InternalProxyIP")
			dnsServer := &hostedclusterv1alpha1.DNSServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: resourceNamespace,
				},
				Spec: hostedclusterv1alpha1.DNSServerSpec{
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
					NetworkConfig: hostedclusterv1alpha1.DNSNetworkConfig{
						ServerIP:             "192.168.100.3",
						ProxyIP:              "192.168.100.10",
						SecondaryNetworkCIDR: "192.168.100.0/24",
						DNSPort:              53,
					},
					UpstreamDNS: []string{"8.8.8.8"},
					Image:       "quay.io/cldmnky/oooi:latest",
				},
			}
			err = k8sClient.Create(ctx, dnsServer)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			By("cleaning up the DNSServer resource")
			dnsServer := &hostedclusterv1alpha1.DNSServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, dnsServer)
			if err == nil {
				Expect(k8sClient.Delete(ctx, dnsServer)).To(Succeed())
			}

			By("cleaning up the namespace")
			namespace := &corev1.Namespace{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceNamespace}, namespace)
			if err == nil {
				// Delete namespace (ignore errors if already being deleted)
				k8sClient.Delete(ctx, namespace)
			}
		})

		It("should create Corefile with default view forwarding only", func() {
			By("reconciling the DNSServer resource")
			controllerReconciler := &DNSServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("fetching the ConfigMap")
			configMap := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      resourceName + "-dns-config",
				Namespace: resourceNamespace,
			}, configMap)
			Expect(err).NotTo(HaveOccurred())

			corefile := configMap.Data["Corefile"]

			By("verifying Corefile contains view plugin structure")
			Expect(corefile).To(ContainSubstring("view multus {"))
			Expect(corefile).To(ContainSubstring("view default {"))

			By("verifying multus view has hosts entries with external proxy IP")
			Expect(corefile).To(MatchRegexp(`view multus[\s\S]*?192\.168\.100\.10\s+api\.my-cluster\.example\.com`))
			Expect(corefile).To(MatchRegexp(`view multus[\s\S]*?192\.168\.100\.10\s+api-int\.my-cluster\.example\.com`))

			By("verifying default view does NOT have hosts plugin")
			// Extract default view section
			defaultStart := strings.Index(corefile, "view default")
			defaultEnd := strings.Index(corefile[defaultStart:], "# Shared plugins")
			if defaultEnd > 0 {
				defaultView := corefile[defaultStart : defaultStart+defaultEnd]
				Expect(defaultView).NotTo(ContainSubstring("hosts {"))
			}

			By("verifying default view only forwards to upstream")
			Expect(corefile).To(MatchRegexp(`view default[\s\S]*?forward[\s\S]*?8\.8\.8\.8`))

			By("verifying internal proxy IP is not present anywhere")
			Expect(corefile).NotTo(ContainSubstring("10.96.100.200"))

			By("verifying external proxy IP only appears in multus view")
			externalIPCount := strings.Count(corefile, "192.168.100.10")
			Expect(externalIPCount).To(Equal(2), "External proxy IP should only appear in multus view (2 endpoints)")
		})
	})

	Context("Service status population", func() {
		const resourceName = "test-service-status"
		const resourceNamespace = "clusters-test-svc-status"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: resourceNamespace,
		}

		BeforeEach(func() {
			By("creating the namespace")
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: resourceNamespace,
				},
			}
			// Try to get the namespace first
			err := k8sClient.Get(ctx, types.NamespacedName{Name: resourceNamespace}, namespace)
			if err != nil && errors.IsNotFound(err) {
				// Namespace doesn't exist, create it
				err = k8sClient.Create(ctx, namespace)
				Expect(err).NotTo(HaveOccurred())
			} else if err == nil {
				// Namespace exists, that's okay
			} else {
				// Some other error occurred
				Expect(err).NotTo(HaveOccurred())
			}

			By("creating the DNSServer resource")
			dnsServer := &hostedclusterv1alpha1.DNSServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: resourceNamespace,
				},
				Spec: hostedclusterv1alpha1.DNSServerSpec{
					HostedClusterDomain: "my-cluster.example.com",
					StaticEntries: []hostedclusterv1alpha1.DNSStaticEntry{
						{
							Hostname: "api.my-cluster.example.com",
							IP:       "192.168.100.10",
						},
					},
					NetworkConfig: hostedclusterv1alpha1.DNSNetworkConfig{
						ServerIP:             "192.168.100.3",
						ProxyIP:              "192.168.100.10",
						SecondaryNetworkCIDR: "192.168.100.0/24",
						DNSPort:              53,
					},
					UpstreamDNS: []string{"8.8.8.8"},
					Image:       "quay.io/cldmnky/oooi:latest",
				},
			}
			err = k8sClient.Create(ctx, dnsServer)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			By("cleaning up the DNSServer resource")
			dnsServer := &hostedclusterv1alpha1.DNSServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, dnsServer)
			if err == nil {
				Expect(k8sClient.Delete(ctx, dnsServer)).To(Succeed())
			}

			By("cleaning up the namespace")
			namespace := &corev1.Namespace{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceNamespace}, namespace)
			if err == nil {
				// Delete namespace (ignore errors if already being deleted)
				k8sClient.Delete(ctx, namespace)
			}
		})

		It("should populate ServiceName and ServiceClusterIP in status", func() {
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

			By("verifying ServiceName is populated")
			Expect(dnsServer.Status.ServiceName).To(Equal(resourceName + "-dns"))

			By("verifying ServiceClusterIP is populated")
			Expect(dnsServer.Status.ServiceClusterIP).NotTo(BeEmpty())

			By("verifying the Service actually exists with correct ClusterIP")
			service := &corev1.Service{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      dnsServer.Status.ServiceName,
				Namespace: resourceNamespace,
			}, service)
			Expect(err).NotTo(HaveOccurred())
			Expect(service.Spec.ClusterIP).To(Equal(dnsServer.Status.ServiceClusterIP))
		})
	})
})

// Helper function to extract regex match from string
func extractMatch(text, pattern string) string {
	re := regexp.MustCompile(pattern)
	match := re.FindString(text)
	return match
}

// Helper function to find a condition by type
func findCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}
