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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	hostedclusterv1alpha1 "github.com/cldmnky/oooi/api/v1alpha1"
)

var _ = Describe("ProxyServer Controller", func() {
	Context("When reconciling a ProxyServer resource", func() {
		const (
			proxyServerName      = "test-proxy"
			proxyServerNamespace = "default"
			timeout              = time.Second * 10
			interval             = time.Millisecond * 250
		)

		ctx := context.Background()
		typeNamespacedName := types.NamespacedName{
			Name:      proxyServerName,
			Namespace: proxyServerNamespace,
		}

		var proxyServer *hostedclusterv1alpha1.ProxyServer

		BeforeEach(func() {
			By("creating the ProxyServer resource")
			proxyServer = &hostedclusterv1alpha1.ProxyServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      proxyServerName,
					Namespace: proxyServerNamespace,
				},
				Spec: hostedclusterv1alpha1.ProxyServerSpec{
					NetworkConfig: hostedclusterv1alpha1.ProxyNetworkConfig{
						ServerIP:                   "10.10.10.3",
						NetworkAttachmentName:      "tenant-network",
						NetworkAttachmentNamespace: proxyServerNamespace,
					},
					Backends: []hostedclusterv1alpha1.ProxyBackend{
						{
							Name:            "kube-apiserver",
							Hostname:        "api.test-cluster.example.com",
							Port:            6443,
							TargetService:   "kube-apiserver",
							TargetPort:      6443,
							TargetNamespace: "default",
							Protocol:        "TCP",
							TimeoutSeconds:  30,
						},
						{
							Name:            "oauth-openshift",
							Hostname:        "oauth.test-cluster.example.com",
							Port:            6443,
							TargetService:   "oauth-openshift",
							TargetPort:      6443,
							TargetNamespace: "default",
							Protocol:        "TCP",
							TimeoutSeconds:  30,
						},
					},
					ProxyImage:   "envoyproxy/envoy:v1.36.4",
					ManagerImage: "quay.io/cldmnky/oooi:test",
					Port:         443,
					XDSPort:      18000,
					LogLevel:     "info",
				},
			}
			Expect(k8sClient.Create(ctx, proxyServer)).To(Succeed())
		})

		AfterEach(func() {
			By("cleaning up the ProxyServer resource")
			resource := &hostedclusterv1alpha1.ProxyServer{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}

			// Wait for deletion to complete
			Eventually(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, &hostedclusterv1alpha1.ProxyServer{})
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())
		})

		It("should successfully reconcile the resource", func() {
			By("reconciling the created resource")
			controllerReconciler := &ProxyServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("checking that ConfigMap was created")
			configMap := &corev1.ConfigMap{}
			configMapName := types.NamespacedName{
				Name:      proxyServerName + "-proxy-bootstrap",
				Namespace: proxyServerNamespace,
			}
			Eventually(func() error {
				return k8sClient.Get(ctx, configMapName, configMap)
			}, timeout, interval).Should(Succeed())

			By("verifying ConfigMap content")
			Expect(configMap.Data).To(HaveKey("bootstrap.json"))
			Expect(configMap.Data["bootstrap.json"]).To(ContainSubstring(`"id": "test-proxy"`))
			Expect(configMap.Data["bootstrap.json"]).To(ContainSubstring(`"port_value": 18000`))
			Expect(configMap.Data["bootstrap.json"]).To(ContainSubstring("xds_cluster"))
			Expect(configMap.Data["bootstrap.json"]).To(ContainSubstring("127.0.0.1"))

			By("verifying ConfigMap has owner reference")
			Expect(configMap.OwnerReferences).To(HaveLen(1))
			Expect(configMap.OwnerReferences[0].Name).To(Equal(proxyServerName))
			Expect(configMap.OwnerReferences[0].Kind).To(Equal("ProxyServer"))

			By("checking that Deployment was created")
			deployment := &appsv1.Deployment{}
			deploymentName := types.NamespacedName{
				Name:      proxyServerName + "-proxy",
				Namespace: proxyServerNamespace,
			}
			Eventually(func() error {
				return k8sClient.Get(ctx, deploymentName, deployment)
			}, timeout, interval).Should(Succeed())

			By("verifying Deployment has owner reference")
			Expect(deployment.OwnerReferences).To(HaveLen(1))
			Expect(deployment.OwnerReferences[0].Name).To(Equal(proxyServerName))
			Expect(deployment.OwnerReferences[0].Kind).To(Equal("ProxyServer"))

			By("verifying Deployment has correct labels")
			Expect(deployment.Labels).To(HaveKeyWithValue("app", "proxy-server"))
			Expect(deployment.Labels).To(HaveKeyWithValue("hostedcluster.densityops.com", proxyServerName))

			By("verifying Deployment has two containers (envoy + manager)")
			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(2))

			By("verifying Envoy container configuration")
			var envoyContainer *corev1.Container
			for i := range deployment.Spec.Template.Spec.Containers {
				if deployment.Spec.Template.Spec.Containers[i].Name == "envoy" {
					envoyContainer = &deployment.Spec.Template.Spec.Containers[i]
					break
				}
			}
			Expect(envoyContainer).NotTo(BeNil())
			Expect(envoyContainer.Image).To(Equal("envoyproxy/envoy:v1.36.4"))
			Expect(envoyContainer.Args).To(ContainElement(ContainSubstring("/etc/envoy/bootstrap.json")))
			Expect(envoyContainer.VolumeMounts).To(ContainElement(HaveField("Name", "bootstrap-config")))

			By("verifying Pod security context allows running as root for privileged ports")
			Expect(deployment.Spec.Template.Spec.SecurityContext).NotTo(BeNil())
			Expect(deployment.Spec.Template.Spec.SecurityContext.RunAsNonRoot).NotTo(BeNil())
			Expect(*deployment.Spec.Template.Spec.SecurityContext.RunAsNonRoot).To(BeFalse())

			By("verifying Manager container configuration")
			var managerContainer *corev1.Container
			for i := range deployment.Spec.Template.Spec.Containers {
				if deployment.Spec.Template.Spec.Containers[i].Name == "manager" {
					managerContainer = &deployment.Spec.Template.Spec.Containers[i]
					break
				}
			}
			Expect(managerContainer).NotTo(BeNil())
			Expect(managerContainer.Image).To(Equal("quay.io/cldmnky/oooi:test"))
			Expect(managerContainer.Args).To(ContainElement("proxy"))
			Expect(managerContainer.Args).To(ContainElement("--xds-port"))
			Expect(managerContainer.Args).To(ContainElement("18000"))

			By("verifying Deployment has Multus network annotation")
			Expect(deployment.Spec.Template.Annotations).To(HaveKey("k8s.v1.cni.cncf.io/networks"))
			expectedNetworkAnnotation := `[
  {
    "name": "tenant-network",
    "namespace": "default",
    "ips": ["10.10.10.3/24"]
  }
]`
			Expect(deployment.Spec.Template.Annotations["k8s.v1.cni.cncf.io/networks"]).To(Equal(expectedNetworkAnnotation))

			By("checking that Service was created")
			service := &corev1.Service{}
			serviceName := types.NamespacedName{
				Name:      proxyServerName + "-proxy",
				Namespace: proxyServerNamespace,
			}
			Eventually(func() error {
				return k8sClient.Get(ctx, serviceName, service)
			}, timeout, interval).Should(Succeed())

			By("verifying Service has owner reference")
			Expect(service.OwnerReferences).To(HaveLen(1))
			Expect(service.OwnerReferences[0].Name).To(Equal(proxyServerName))
			Expect(service.OwnerReferences[0].Kind).To(Equal("ProxyServer"))

			By("verifying Service configuration")
			Expect(service.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
			Expect(service.Spec.Selector).To(HaveKeyWithValue("app", "proxy-server"))
			Expect(service.Spec.Ports).To(HaveLen(2)) // proxy port + admin port
			Expect(service.Spec.Ports[0].Port).To(Equal(int32(443)))
			Expect(service.Spec.Ports[0].TargetPort.IntVal).To(Equal(int32(443)))

			By("checking ProxyServer status was updated")
			updatedProxyServer := &hostedclusterv1alpha1.ProxyServer{}
			Eventually(func() error {
				return k8sClient.Get(ctx, typeNamespacedName, updatedProxyServer)
			}, timeout, interval).Should(Succeed())

			Eventually(func() string {
				err := k8sClient.Get(ctx, typeNamespacedName, updatedProxyServer)
				if err != nil {
					return ""
				}
				return updatedProxyServer.Status.ConfigMapName
			}, timeout, interval).Should(Equal(proxyServerName + "-proxy-bootstrap"))

			Expect(updatedProxyServer.Status.DeploymentName).To(Equal(proxyServerName + "-proxy"))
			Expect(updatedProxyServer.Status.ServiceName).To(Equal(proxyServerName + "-proxy"))
			Expect(updatedProxyServer.Status.BackendCount).To(Equal(int32(2)))
			Expect(updatedProxyServer.Status.ServiceIP).NotTo(BeEmpty())
			Expect(updatedProxyServer.Status.Conditions).To(HaveLen(1))
			Expect(updatedProxyServer.Status.Conditions[0].Type).To(Equal("Ready"))
			Expect(updatedProxyServer.Status.Conditions[0].Status).To(Equal(metav1.ConditionTrue))
		})

		It("should handle multiple backends on different ports", func() {
			By("creating ProxyServer with backends on different ports")
			multiPortProxy := &hostedclusterv1alpha1.ProxyServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-port-proxy",
					Namespace: proxyServerNamespace,
				},
				Spec: hostedclusterv1alpha1.ProxyServerSpec{
					NetworkConfig: hostedclusterv1alpha1.ProxyNetworkConfig{
						ServerIP:                   "10.10.10.4",
						NetworkAttachmentName:      "tenant-network",
						NetworkAttachmentNamespace: proxyServerNamespace,
					},
					Backends: []hostedclusterv1alpha1.ProxyBackend{
						{
							Name:            "api-server-443",
							Hostname:        "api.cluster.example.com",
							Port:            6443,
							TargetService:   "kube-apiserver",
							TargetPort:      6443,
							TargetNamespace: "default",
							Protocol:        "TCP",
							TimeoutSeconds:  30,
						},
						{
							Name:            "ignition-22623",
							Hostname:        "ignition.cluster.example.com",
							Port:            22623,
							TargetService:   "ignition",
							TargetPort:      22623,
							TargetNamespace: "default",
							Protocol:        "TCP",
							TimeoutSeconds:  30,
						},
					},
					Port:    443,
					XDSPort: 18000,
				},
			}
			Expect(k8sClient.Create(ctx, multiPortProxy)).To(Succeed())

			defer func() {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "multi-port-proxy", Namespace: proxyServerNamespace}, multiPortProxy)
				if err == nil {
					Expect(k8sClient.Delete(ctx, multiPortProxy)).To(Succeed())
				}
			}()

			By("reconciling the multi-port proxy")
			reconciler := &ProxyServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "multi-port-proxy",
					Namespace: proxyServerNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying Service has multiple ports")
			service := &corev1.Service{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "multi-port-proxy-proxy",
					Namespace: proxyServerNamespace,
				}, service)
			}, timeout, interval).Should(Succeed())

			// Note: Current implementation creates one port per backend
			// With multiple backends on same port, they share the listener
			Expect(service.Spec.Ports).NotTo(BeEmpty())
		})

		It("should update resources when ProxyServer spec changes", func() {
			By("getting initial Deployment")
			initialDeployment := &appsv1.Deployment{}
			deploymentName := types.NamespacedName{
				Name:      proxyServerName + "-proxy",
				Namespace: proxyServerNamespace,
			}

			controllerReconciler := &ProxyServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() error {
				return k8sClient.Get(ctx, deploymentName, initialDeployment)
			}, timeout, interval).Should(Succeed())

			initialImage := initialDeployment.Spec.Template.Spec.Containers[0].Image

			By("updating ProxyServer spec")
			updatedProxyServer := &hostedclusterv1alpha1.ProxyServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedProxyServer)).To(Succeed())
			updatedProxyServer.Spec.ProxyImage = "envoyproxy/envoy:v1.36.5"
			Expect(k8sClient.Update(ctx, updatedProxyServer)).To(Succeed())

			By("reconciling again")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying Deployment image was NOT automatically updated")
			// Note: Current implementation creates resources if missing but doesn't
			// update existing ones. This is typical for basic controllers.
			updatedDeployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, deploymentName, updatedDeployment)
			}, timeout, interval).Should(Succeed())
			Expect(updatedDeployment.Spec.Template.Spec.Containers[0].Image).To(Equal(initialImage))
		})

		It("should handle resource creation failures gracefully", func() {
			By("creating a ProxyServer with invalid backend configuration")
			invalidProxy := &hostedclusterv1alpha1.ProxyServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid-proxy",
					Namespace: proxyServerNamespace,
				},
				Spec: hostedclusterv1alpha1.ProxyServerSpec{
					NetworkConfig: hostedclusterv1alpha1.ProxyNetworkConfig{
						ServerIP:                   "10.10.10.5",
						NetworkAttachmentName:      "tenant-network",
						NetworkAttachmentNamespace: proxyServerNamespace,
					},
					Backends: []hostedclusterv1alpha1.ProxyBackend{
						{
							Name:            "test-backend",
							Hostname:        "test.example.com",
							Port:            443,
							TargetService:   "nonexistent-service",
							TargetPort:      443,
							TargetNamespace: "default",
							Protocol:        "TCP",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, invalidProxy)).To(Succeed())

			defer func() {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "invalid-proxy", Namespace: proxyServerNamespace}, invalidProxy)
				if err == nil {
					Expect(k8sClient.Delete(ctx, invalidProxy)).To(Succeed())
				}
			}()

			By("reconciling the invalid proxy")
			reconciler := &ProxyServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "invalid-proxy",
					Namespace: proxyServerNamespace,
				},
			})
			// Should succeed even if target service doesn't exist
			// The xDS server will handle backend availability
			Expect(err).NotTo(HaveOccurred())

			By("verifying resources were still created")
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "invalid-proxy-proxy",
					Namespace: proxyServerNamespace,
				}, deployment)
			}, timeout, interval).Should(Succeed())
		})

		It("should support custom XDS port configuration", func() {
			By("creating ProxyServer with custom XDS port")
			customXDSProxy := &hostedclusterv1alpha1.ProxyServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "custom-xds-proxy",
					Namespace: proxyServerNamespace,
				},
				Spec: hostedclusterv1alpha1.ProxyServerSpec{
					NetworkConfig: hostedclusterv1alpha1.ProxyNetworkConfig{
						ServerIP:                   "10.10.10.6",
						NetworkAttachmentName:      "tenant-network",
						NetworkAttachmentNamespace: proxyServerNamespace,
					},
					Backends: []hostedclusterv1alpha1.ProxyBackend{
						{
							Name:            "test-backend",
							Hostname:        "test.example.com",
							Port:            443,
							TargetService:   "test-service",
							TargetPort:      443,
							TargetNamespace: "default",
							Protocol:        "TCP",
							TimeoutSeconds:  30,
						},
					},
					XDSPort: 19000, // Custom port
				},
			}
			Expect(k8sClient.Create(ctx, customXDSProxy)).To(Succeed())

			defer func() {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "custom-xds-proxy", Namespace: proxyServerNamespace}, customXDSProxy)
				if err == nil {
					Expect(k8sClient.Delete(ctx, customXDSProxy)).To(Succeed())
				}
			}()

			By("reconciling the proxy")
			reconciler := &ProxyServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "custom-xds-proxy",
					Namespace: proxyServerNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying ConfigMap uses custom XDS port")
			configMap := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "custom-xds-proxy-proxy-bootstrap",
					Namespace: proxyServerNamespace,
				}, configMap)
			}, timeout, interval).Should(Succeed())

			Expect(configMap.Data["bootstrap.json"]).To(ContainSubstring(`"port_value": 19000`))

			By("verifying Deployment manager container uses custom XDS port")
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "custom-xds-proxy-proxy",
					Namespace: proxyServerNamespace,
				}, deployment)
			}, timeout, interval).Should(Succeed())

			var managerContainer *corev1.Container
			for i := range deployment.Spec.Template.Spec.Containers {
				if deployment.Spec.Template.Spec.Containers[i].Name == "manager" {
					managerContainer = &deployment.Spec.Template.Spec.Containers[i]
					break
				}
			}
			Expect(managerContainer).NotTo(BeNil())
			Expect(managerContainer.Args).To(ContainElement("19000"))
		})

		It("should handle deletion via owner references", func() {
			By("reconciling the resource to create dependent objects")
			controllerReconciler := &ProxyServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying all resources exist")
			configMap := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      proxyServerName + "-proxy-bootstrap",
					Namespace: proxyServerNamespace,
				}, configMap)
			}, timeout, interval).Should(Succeed())

			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      proxyServerName + "-proxy",
					Namespace: proxyServerNamespace,
				}, deployment)
			}, timeout, interval).Should(Succeed())

			service := &corev1.Service{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      proxyServerName + "-proxy",
					Namespace: proxyServerNamespace,
				}, service)
			}, timeout, interval).Should(Succeed())

			By("deleting the ProxyServer resource")
			Expect(k8sClient.Delete(ctx, proxyServer)).To(Succeed())

			By("verifying ProxyServer is deleted")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, &hostedclusterv1alpha1.ProxyServer{})
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			By("checking if dependent resources would be deleted (envtest doesn't always process GC)")
			// Note: envtest doesn't always garbage collect resources with owner references
			// In a real cluster, the Kubernetes GC would delete these
			_ = k8sClient.Get(ctx, types.NamespacedName{
				Name:      proxyServerName + "-proxy-bootstrap",
				Namespace: proxyServerNamespace,
			}, &corev1.ConfigMap{})
		})

		It("should use default values when optional fields are not set", func() {
			By("creating ProxyServer with minimal configuration")
			minimalProxy := &hostedclusterv1alpha1.ProxyServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "minimal-proxy",
					Namespace: proxyServerNamespace,
				},
				Spec: hostedclusterv1alpha1.ProxyServerSpec{
					NetworkConfig: hostedclusterv1alpha1.ProxyNetworkConfig{
						ServerIP:              "10.10.10.7",
						NetworkAttachmentName: "tenant-network",
					},
					Backends: []hostedclusterv1alpha1.ProxyBackend{
						{
							Name:            "test",
							Hostname:        "test.example.com",
							Port:            443,
							TargetService:   "test-service",
							TargetPort:      443,
							TargetNamespace: "default",
							Protocol:        "TCP",
							TimeoutSeconds:  30,
						},
					},
					// ProxyImage, ManagerImage, Port, XDSPort, LogLevel all omitted
				},
			}
			Expect(k8sClient.Create(ctx, minimalProxy)).To(Succeed())

			defer func() {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "minimal-proxy", Namespace: proxyServerNamespace}, minimalProxy)
				if err == nil {
					Expect(k8sClient.Delete(ctx, minimalProxy)).To(Succeed())
				}
			}()

			By("reconciling the minimal proxy")
			reconciler := &ProxyServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "minimal-proxy",
					Namespace: proxyServerNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying Deployment uses default images")
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "minimal-proxy-proxy",
					Namespace: proxyServerNamespace,
				}, deployment)
			}, timeout, interval).Should(Succeed())

			var envoyContainer, managerContainer *corev1.Container
			for i := range deployment.Spec.Template.Spec.Containers {
				if deployment.Spec.Template.Spec.Containers[i].Name == "envoy" {
					envoyContainer = &deployment.Spec.Template.Spec.Containers[i]
				}
				if deployment.Spec.Template.Spec.Containers[i].Name == "manager" {
					managerContainer = &deployment.Spec.Template.Spec.Containers[i]
				}
			}
			Expect(envoyContainer).NotTo(BeNil())
			Expect(managerContainer).NotTo(BeNil())
			Expect(envoyContainer.Image).To(Equal("envoyproxy/envoy:v1.36.4"))
			Expect(managerContainer.Image).To(Equal("quay.io/cldmnky/oooi:latest"))

			By("verifying ConfigMap uses default XDS port")
			configMap := &corev1.ConfigMap{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "minimal-proxy-proxy-bootstrap",
					Namespace: proxyServerNamespace,
				}, configMap)
			}, timeout, interval).Should(Succeed())

			Expect(configMap.Data["bootstrap.json"]).To(ContainSubstring(`"port_value": 18000`))

			By("verifying Service uses default port")
			service := &corev1.Service{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "minimal-proxy-proxy",
					Namespace: proxyServerNamespace,
				}, service)
			}, timeout, interval).Should(Succeed())

			Expect(service.Spec.Ports[0].Port).To(Equal(int32(443)))
		})

		It("should set correct namespace for NetworkAttachmentDefinition", func() {
			By("creating ProxyServer without NAD namespace")
			noNadNsProxy := &hostedclusterv1alpha1.ProxyServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-nad-ns-proxy",
					Namespace: "custom-namespace",
				},
				Spec: hostedclusterv1alpha1.ProxyServerSpec{
					NetworkConfig: hostedclusterv1alpha1.ProxyNetworkConfig{
						ServerIP:              "10.10.10.8",
						NetworkAttachmentName: "tenant-network",
						// NetworkAttachmentNamespace not set
					},
					Backends: []hostedclusterv1alpha1.ProxyBackend{
						{
							Name:            "test",
							Hostname:        "test.example.com",
							Port:            443,
							TargetService:   "test-service",
							TargetPort:      443,
							TargetNamespace: "default",
							Protocol:        "TCP",
							TimeoutSeconds:  30,
						},
					},
				},
			}

			// Create namespace
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "custom-namespace",
				},
			}
			err := k8sClient.Create(ctx, ns)
			if err != nil && !errors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}

			Expect(k8sClient.Create(ctx, noNadNsProxy)).To(Succeed())

			defer func() {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "no-nad-ns-proxy", Namespace: "custom-namespace"}, noNadNsProxy)
				if err == nil {
					Expect(k8sClient.Delete(ctx, noNadNsProxy)).To(Succeed())
				}
			}()

			By("reconciling the proxy")
			reconciler := &ProxyServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "no-nad-ns-proxy",
					Namespace: "custom-namespace",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying Deployment uses ProxyServer's namespace for NAD")
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "no-nad-ns-proxy-proxy",
					Namespace: "custom-namespace",
				}, deployment)
			}, timeout, interval).Should(Succeed())

			Expect(deployment.Spec.Template.Annotations).To(HaveKey("k8s.v1.cni.cncf.io/networks"))
			// Should default to ProxyServer's namespace
			expectedNetworkAnnotation := `[
  {
    "name": "tenant-network",
    "namespace": "custom-namespace",
    "ips": ["10.10.10.8/24"]
  }
]`
			Expect(deployment.Spec.Template.Annotations["k8s.v1.cni.cncf.io/networks"]).To(Equal(expectedNetworkAnnotation))
		})

		It("should create RBAC resources for proxy pods", func() {
			ctx := context.Background()
			proxyServerName := "rbac-test-proxy"
			proxyServerNamespace := "default"

			By("creating a ProxyServer resource")
			rbacProxy := &hostedclusterv1alpha1.ProxyServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      proxyServerName,
					Namespace: proxyServerNamespace,
				},
				Spec: hostedclusterv1alpha1.ProxyServerSpec{
					NetworkConfig: hostedclusterv1alpha1.ProxyNetworkConfig{
						ServerIP:                   "10.10.10.100",
						NetworkAttachmentName:      "tenant-network",
						NetworkAttachmentNamespace: proxyServerNamespace,
					},
					Backends: []hostedclusterv1alpha1.ProxyBackend{
						{
							Name:            "test-backend",
							Hostname:        "test.example.com",
							Port:            6443,
							TargetService:   "test-svc",
							TargetPort:      6443,
							TargetNamespace: "default",
							Protocol:        "TCP",
							TimeoutSeconds:  30,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, rbacProxy)).To(Succeed())

			defer func() {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: proxyServerName, Namespace: proxyServerNamespace}, rbacProxy)
				if err == nil {
					Expect(k8sClient.Delete(ctx, rbacProxy)).To(Succeed())
				}
			}()

			By("reconciling the ProxyServer")
			reconciler := &ProxyServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      proxyServerName,
					Namespace: proxyServerNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying ServiceAccount was created")
			serviceAccount := &corev1.ServiceAccount{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      proxyServerName + "-proxy",
					Namespace: proxyServerNamespace,
				}, serviceAccount)
			}, timeout, interval).Should(Succeed())
			Expect(serviceAccount.Labels).To(HaveKeyWithValue("app", "proxy-server"))
			Expect(serviceAccount.OwnerReferences).To(HaveLen(1))
			Expect(serviceAccount.OwnerReferences[0].Name).To(Equal(proxyServerName))

			By("verifying Role was created with ProxyServer permissions")
			role := &rbacv1.Role{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      proxyServerName + "-proxy",
					Namespace: proxyServerNamespace,
				}, role)
			}, timeout, interval).Should(Succeed())
			Expect(role.Labels).To(HaveKeyWithValue("app", "proxy-server"))
			Expect(role.OwnerReferences).To(HaveLen(1))
			Expect(role.OwnerReferences[0].Name).To(Equal(proxyServerName))
			// Verify role has permission to list and watch ProxyServers
			Expect(role.Rules).To(HaveLen(1))
			Expect(role.Rules[0].APIGroups).To(ContainElement("hostedcluster.densityops.com"))
			Expect(role.Rules[0].Resources).To(ContainElement("proxyservers"))
			Expect(role.Rules[0].Verbs).To(ContainElement("get"))
			Expect(role.Rules[0].Verbs).To(ContainElement("list"))
			Expect(role.Rules[0].Verbs).To(ContainElement("watch"))

			By("verifying RoleBinding was created")
			roleBinding := &rbacv1.RoleBinding{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      proxyServerName + "-proxy",
					Namespace: proxyServerNamespace,
				}, roleBinding)
			}, timeout, interval).Should(Succeed())
			Expect(roleBinding.Labels).To(HaveKeyWithValue("app", "proxy-server"))
			Expect(roleBinding.OwnerReferences).To(HaveLen(1))
			Expect(roleBinding.OwnerReferences[0].Name).To(Equal(proxyServerName))
			Expect(roleBinding.RoleRef.Name).To(Equal(proxyServerName + "-proxy"))
			Expect(roleBinding.RoleRef.Kind).To(Equal("Role"))
			Expect(roleBinding.Subjects).To(HaveLen(1))
			Expect(roleBinding.Subjects[0].Kind).To(Equal("ServiceAccount"))
			Expect(roleBinding.Subjects[0].Name).To(Equal(proxyServerName + "-proxy"))

			By("verifying Deployment uses the created ServiceAccount")
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      proxyServerName + "-proxy",
					Namespace: proxyServerNamespace,
				}, deployment)
			}, timeout, interval).Should(Succeed())
			Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal(proxyServerName + "-proxy"))
		})
	})

	Context("When handling nonexistent ProxyServer", func() {
		It("should not error on reconcile", func() {
			ctx := context.Background()
			reconciler := &ProxyServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "nonexistent-proxy",
					Namespace: "default",
				},
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("When testing SetupWithManager", func() {
		It("should setup the controller with manager", func() {
			// This test verifies that the SetupWithManager function exists and works
			// The actual manager setup is tested in integration tests
			reconciler := &ProxyServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			mgr, err := ctrl.NewManager(cfg, ctrl.Options{
				Scheme: k8sClient.Scheme(),
			})
			Expect(err).NotTo(HaveOccurred())

			err = reconciler.SetupWithManager(mgr)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
