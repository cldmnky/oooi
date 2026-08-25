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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	hostedclusterv1alpha1 "github.com/cldmnky/oooi/api/v1alpha1"
)

var _ = Describe("Infra Controller", func() {
	ctx := context.Background()

	It("does not configure an external Service on the shared ProxyServer", func() {
		infra := &hostedclusterv1alpha1.Infra{
			ObjectMeta: metav1.ObjectMeta{Name: "myinfra", Namespace: "clusters"},
			Spec: hostedclusterv1alpha1.InfraSpec{
				NetworkConfig: hostedclusterv1alpha1.NetworkConfig{NetworkAttachmentDefinition: "vlan100"},
			},
		}
		proxyServer := (&InfraReconciler{}).proxyServerForInfra(infra, nil)
		Expect(proxyServer.Spec.ExternalService).To(Equal(hostedclusterv1alpha1.ProxyExternalService{}))
	})

	Context("with an explicit attachment", func() {
		const resourceName = "test-resource"
		resourceKey := types.NamespacedName{Name: resourceName, Namespace: "default"}
		attachmentName := resourceName + "-attachment"

		BeforeEach(func() {
			infra := &hostedclusterv1alpha1.Infra{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"},
				Spec: hostedclusterv1alpha1.InfraSpec{
					NetworkConfig: hostedclusterv1alpha1.NetworkConfig{
						CIDR:                        "192.168.100.0/24",
						Gateway:                     "192.168.100.1",
						NetworkAttachmentDefinition: "tenant-vlan-100",
						DNSServers:                  []string{"8.8.8.8", "8.8.4.4"},
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
							Enabled:  true,
							ServerIP: "192.168.100.3",
						},
						Proxy: hostedclusterv1alpha1.ProxyConfig{
							Enabled:  true,
							ServerIP: "192.168.100.4",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, infra)).To(Succeed())
			Expect(k8sClient.Create(ctx, makeAttachment(
				attachmentName, resourceName, "test-cluster", "example.com", "clusters-test",
			))).To(Succeed())
		})

		AfterEach(func() {
			attachment := &hostedclusterv1alpha1.InfraClusterAttachment{}
			Expect(client.IgnoreNotFound(k8sClient.Get(ctx, types.NamespacedName{
				Name: attachmentName, Namespace: "default",
			}, attachment))).To(Succeed())
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, attachment))).To(Succeed())

			infra := &hostedclusterv1alpha1.Infra{}
			if err := k8sClient.Get(ctx, resourceKey, infra); err == nil {
				Expect(k8sClient.Delete(ctx, infra)).To(Succeed())
			}
			for _, obj := range []client.Object{
				&hostedclusterv1alpha1.DHCPServer{ObjectMeta: metav1.ObjectMeta{Name: resourceName + "-dhcp", Namespace: "default"}},
				&hostedclusterv1alpha1.DNSServer{ObjectMeta: metav1.ObjectMeta{Name: resourceName + "-dns", Namespace: "default"}},
				&hostedclusterv1alpha1.ProxyServer{ObjectMeta: metav1.ObjectMeta{Name: resourceName + "-proxy", Namespace: "default"}},
			} {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			}
		})

		It("creates one shared child set and reports valid component configuration", func() {
			r := &InfraReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: resourceKey})
			Expect(err).NotTo(HaveOccurred())

			for _, obj := range []client.Object{
				&hostedclusterv1alpha1.DHCPServer{},
				&hostedclusterv1alpha1.DNSServer{},
				&hostedclusterv1alpha1.ProxyServer{},
			} {
				Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: resourceName + suffixForKind(obj), Namespace: "default",
				}, obj)).To(Succeed())
				Expect(obj.GetOwnerReferences()).To(HaveLen(1))
				Expect(obj.GetOwnerReferences()[0].Name).To(Equal(resourceName))
			}

			infra := &hostedclusterv1alpha1.Infra{}
			Expect(k8sClient.Get(ctx, resourceKey, infra)).To(Succeed())
			Expect(infra.Status.ComponentStatus.DHCPReady).To(BeTrue())
			Expect(infra.Status.ComponentStatus.DNSReady).To(BeTrue())
			Expect(infra.Status.ComponentStatus.ProxyReady).To(BeTrue())

			proxy := &hostedclusterv1alpha1.ProxyServer{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: resourceName + "-proxy", Namespace: "default",
			}, proxy)).To(Succeed())
			Expect(proxy.Spec.Backends).NotTo(BeEmpty())
			for _, backend := range proxy.Spec.Backends {
				Expect(backend.AlternateHostnames).To(BeEmpty())
			}
		})

		It("clears component readiness when a component is disabled", func() {
			r := &InfraReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: resourceKey})
			Expect(err).NotTo(HaveOccurred())

			infra := &hostedclusterv1alpha1.Infra{}
			Expect(k8sClient.Get(ctx, resourceKey, infra)).To(Succeed())
			infra.Spec.InfraComponents.DHCP.Enabled = false
			infra.Spec.InfraComponents.DNS.Enabled = false
			infra.Spec.InfraComponents.Proxy.Enabled = false
			Expect(k8sClient.Update(ctx, infra)).To(Succeed())

			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: resourceKey})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, resourceKey, infra)).To(Succeed())
			Expect(infra.Status.ComponentStatus.DHCPReady).To(BeFalse())
			Expect(infra.Status.ComponentStatus.DNSReady).To(BeFalse())
			Expect(infra.Status.ComponentStatus.ProxyReady).To(BeFalse())
		})

		It("uses the Infra namespace when no NAD namespace is configured", func() {
			r := &InfraReconciler{}
			dhcp := r.dhcpServerForInfra(&hostedclusterv1alpha1.Infra{
				ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "infra-ns"},
				Spec: hostedclusterv1alpha1.InfraSpec{
					NetworkConfig: hostedclusterv1alpha1.NetworkConfig{NetworkAttachmentDefinition: "vlan"},
				},
			})
			Expect(dhcp.Spec.NetworkConfig.NetworkAttachmentNamespace).To(Equal("infra-ns"))
		})

		It("does not create DNS or proxy children without attachments", func() {
			name := "no-attachments"
			infra := &hostedclusterv1alpha1.Infra{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
				Spec: hostedclusterv1alpha1.InfraSpec{
					NetworkConfig: hostedclusterv1alpha1.NetworkConfig{
						CIDR:                        "192.0.2.0/24",
						Gateway:                     "192.0.2.1",
						NetworkAttachmentDefinition: "vlan",
					},
					InfraComponents: hostedclusterv1alpha1.InfraComponents{
						DNS:   hostedclusterv1alpha1.DNSConfig{Enabled: true, ServerIP: "192.0.2.3"},
						Proxy: hostedclusterv1alpha1.ProxyConfig{Enabled: true, ServerIP: "192.0.2.4"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, infra)).To(Succeed())
			r := &InfraReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{
				Name: name, Namespace: "default",
			}})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name + "-dns", Namespace: "default"}, &hostedclusterv1alpha1.DNSServer{})).To(MatchError(ContainSubstring("not found")))
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name + "-proxy", Namespace: "default"}, &hostedclusterv1alpha1.ProxyServer{})).To(MatchError(ContainSubstring("not found")))
			updated := &hostedclusterv1alpha1.Infra{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, updated)).To(Succeed())
			Expect(updated.Status.ComponentStatus.ProxyReady).To(BeFalse())
			Expect(k8sClient.Delete(ctx, infra)).To(Succeed())
		})
	})
})

func suffixForKind(obj client.Object) string {
	switch obj.(type) {
	case *hostedclusterv1alpha1.DHCPServer:
		return "-dhcp"
	case *hostedclusterv1alpha1.DNSServer:
		return "-dns"
	case *hostedclusterv1alpha1.ProxyServer:
		return "-proxy"
	default:
		return ""
	}
}
