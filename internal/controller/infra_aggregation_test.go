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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	hostedclusterv1alpha1 "github.com/cldmnky/oooi/api/v1alpha1"
)

func makeAttachment(name, infraName, clusterName, baseDomain, cpns string) *hostedclusterv1alpha1.InfraClusterAttachment {
	return &hostedclusterv1alpha1.InfraClusterAttachment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: hostedclusterv1alpha1.InfraClusterAttachmentSpec{
			InfraRef:         hostedclusterv1alpha1.InfraReference{Name: infraName},
			HostedClusterRef: hostedclusterv1alpha1.HostedClusterReference{Name: name, Namespace: "clusters"},
			DNS: hostedclusterv1alpha1.AttachmentDNSConfig{
				ClusterName: clusterName,
				BaseDomain:  baseDomain,
			},
			ControlPlaneNamespace: cpns,
		},
	}
}

func ensureNamespace(ctx context.Context, name string) {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	err := k8sClient.Get(ctx, client.ObjectKeyFromObject(ns), ns)
	if errors.IsNotFound(err) {
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
	} else {
		Expect(err).NotTo(HaveOccurred())
	}
}

var _ = Describe("Infra multi-cluster aggregation", func() {
	ctx := context.Background()

	var reconciler *InfraReconciler
	const infraName = "shared-vlan"
	infraKey := types.NamespacedName{Name: infraName, Namespace: "default"}

	createSharedInfra := func(withLegacyFields bool) {
		spec := hostedclusterv1alpha1.InfraSpec{
			NetworkConfig: hostedclusterv1alpha1.NetworkConfig{
				CIDR:                        "192.168.100.0/24",
				Gateway:                     "192.168.100.1",
				NetworkAttachmentDefinition: "vlan100",
			},
			InfraComponents: hostedclusterv1alpha1.InfraComponents{
				DHCP: hostedclusterv1alpha1.DHCPConfig{
					Enabled:    true,
					ServerIP:   "192.168.100.2",
					RangeStart: "192.168.100.10",
					RangeEnd:   "192.168.100.200",
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
		}
		if withLegacyFields {
			spec.InfraComponents.DNS.ClusterName = "legacy"
			spec.InfraComponents.DNS.BaseDomain = "example.com"
			spec.InfraComponents.Proxy.ControlPlaneNamespace = "clusters-legacy"
			ensureNamespace(ctx, "clusters-legacy")
		}
		Expect(k8sClient.Create(ctx, &hostedclusterv1alpha1.Infra{
			ObjectMeta: metav1.ObjectMeta{Name: infraName, Namespace: "default"},
			Spec:       spec,
		})).To(Succeed())
	}

	reconcileInfra := func() {
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: infraKey})
		Expect(err).NotTo(HaveOccurred())
	}

	getDNS := func() hostedclusterv1alpha1.DNSServer {
		var dns hostedclusterv1alpha1.DNSServer
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: infraName + "-dns", Namespace: "default"}, &dns)).To(Succeed())
		return dns
	}

	getProxy := func() hostedclusterv1alpha1.ProxyServer {
		var proxy hostedclusterv1alpha1.ProxyServer
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: infraName + "-proxy", Namespace: "default"}, &proxy)).To(Succeed())
		return proxy
	}

	getInfra := func() hostedclusterv1alpha1.Infra {
		var infra hostedclusterv1alpha1.Infra
		Expect(k8sClient.Get(ctx, infraKey, &infra)).To(Succeed())
		return infra
	}

	AfterEach(func() {
		// Remove attachments first, then the Infra, mirroring documented teardown.
		var atts hostedclusterv1alpha1.InfraClusterAttachmentList
		Expect(k8sClient.List(ctx, &atts, client.InNamespace("default"))).To(Succeed())
		for i := range atts.Items {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &atts.Items[i]))).To(Succeed())
		}
		infra := &hostedclusterv1alpha1.Infra{ObjectMeta: metav1.ObjectMeta{Name: infraName, Namespace: "default"}}
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, infra))).To(Succeed())

		// envtest has no GC; delete the reconciler-created children explicitly.
		for _, n := range []string{infraName + "-dhcp", infraName + "-dns", infraName + "-proxy"} {
			for _, obj := range []client.Object{
				&hostedclusterv1alpha1.DHCPServer{ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: "default"}},
				&hostedclusterv1alpha1.DNSServer{ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: "default"}},
				&hostedclusterv1alpha1.ProxyServer{ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: "default"}},
			} {
				_ = k8sClient.Delete(ctx, obj)
			}
		}
	})

	BeforeEach(func() {
		reconciler = &InfraReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
	})

	It("aggregates two attachments into one shared child set regardless of creation order", func() {
		createSharedInfra(false)
		// Deliberately out of alphabetical order to prove deterministic output.
		Expect(k8sClient.Create(ctx, makeAttachment("bravo", infraName, "bravo", "example.com", "clusters-bravo"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeAttachment("alpha", infraName, "alpha", "example.com", "clusters-alpha"))).To(Succeed())

		reconcileInfra()

		By("creating exactly one child per component")
		prefix := infraName + "-"
		countWithPrefix := func(names []string) int {
			c := 0
			for _, nm := range names {
				if strings.HasPrefix(nm, prefix) {
					c++
				}
			}
			return c
		}

		var dhcs hostedclusterv1alpha1.DHCPServerList
		var dnss hostedclusterv1alpha1.DNSServerList
		var prxs hostedclusterv1alpha1.ProxyServerList
		Expect(k8sClient.List(ctx, &dhcs)).To(Succeed())
		Expect(k8sClient.List(ctx, &dnss)).To(Succeed())
		Expect(k8sClient.List(ctx, &prxs)).To(Succeed())

		dhcpNames := make([]string, 0, len(dhcs.Items))
		for i := range dhcs.Items {
			dhcpNames = append(dhcpNames, dhcs.Items[i].Name)
		}
		dnsNames := make([]string, 0, len(dnss.Items))
		for i := range dnss.Items {
			dnsNames = append(dnsNames, dnss.Items[i].Name)
		}
		prxNames := make([]string, 0, len(prxs.Items))
		for i := range prxs.Items {
			prxNames = append(prxNames, prxs.Items[i].Name)
		}
		Expect(countWithPrefix(dhcpNames)).To(Equal(1), "one DHCPServer for this Infra")
		Expect(countWithPrefix(dnsNames)).To(Equal(1), "one DNSServer for this Infra")
		Expect(countWithPrefix(prxNames)).To(Equal(1), "one ProxyServer for this Infra")

		By("publishing static records for both cluster domains")
		dns := getDNS()
		hostnames := map[string]string{}
		for _, e := range dns.Spec.StaticEntries {
			hostnames[e.Hostname] = e.IP
		}
		Expect(hostnames).To(HaveKeyWithValue("api.alpha.example.com", "192.168.100.4"))
		Expect(hostnames).To(HaveKeyWithValue("konnectivity.bravo.example.com", "192.168.100.4"))

		By("routing SNI backends to each control-plane namespace")
		proxy := getProxy()
		byName := map[string]hostedclusterv1alpha1.ProxyBackend{}
		for _, b := range proxy.Spec.Backends {
			byName[b.Name] = b
		}
		Expect(byName).To(HaveKey("alpha-kube-apiserver"))
		Expect(byName["alpha-kube-apiserver"].TargetNamespace).To(Equal("clusters-alpha"))
		Expect(byName).To(HaveKey("bravo-konnectivity-server"))
		Expect(byName["bravo-konnectivity-server"].TargetNamespace).To(Equal("clusters-bravo"))

		By("omitting ambiguous unqualified Kubernetes aliases")
		for _, b := range proxy.Spec.Backends {
			for _, alt := range b.AlternateHostnames {
				Expect(alt).NotTo(Equal("kubernetes.default"))
			}
		}
		Expect(byName).NotTo(HaveKey("kube-apiserver-kubernetes-hostname"))

		By("summarizing attachment readiness")
		summary := getInfra().Status.Attachments
		Expect(summary.Total).To(Equal(int32(2)))
		Expect(summary.Ready).To(Equal(int32(0)))
		Expect(summary.LegacyFieldsIgnored).To(BeFalse())
	})

	It("marks conflicting domains Degraded and excludes both", func() {
		createSharedInfra(false)
		Expect(k8sClient.Create(ctx, makeAttachment("first", infraName, "dupe", "collide.example.com", "clusters-first"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeAttachment("second", infraName, "dupe", "collide.example.com", "clusters-second"))).To(Succeed())

		reconcileInfra()

		cond := meta.FindStatusCondition(getInfra().Status.Conditions, "Ready")
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(reasonDuplicateHostname))
		Expect(cond.Message).To(ContainSubstring("first"))
		Expect(cond.Message).To(ContainSubstring("second"))

		for _, e := range getDNS().Spec.StaticEntries {
			Expect(e.Hostname).NotTo(ContainSubstring("collide.example.com"))
		}
	})

	It("ignores legacy cluster fields when explicit attachments exist", func() {
		createSharedInfra(true)
		Expect(k8sClient.Create(ctx, makeAttachment("only", infraName, "only", "example.com", "clusters-only"))).To(Succeed())

		reconcileInfra()

		Expect(getInfra().Status.Attachments.LegacyFieldsIgnored).To(BeTrue())
		for _, e := range getDNS().Spec.StaticEntries {
			Expect(e.Hostname).NotTo(ContainSubstring("legacy.example.com"))
		}
		Expect(getProxy().Spec.Backends[0].Hostname).To(HaveSuffix("only.example.com"))
	})

	It("keeps the historical single-cluster behavior with no attachments", func() {
		createSharedInfra(true)
		reconcileInfra()

		dns := getDNS()
		found := false
		for _, e := range dns.Spec.StaticEntries {
			if e.Hostname == "api.legacy.example.com" {
				found = true
			}
		}
		Expect(found).To(BeTrue(), "implicit binding must preserve legacy records")
		Expect(dns.Spec.HostedClusterDomain).To(Equal("legacy.example.com"))
	})
})

var _ = Describe("InfraClusterAttachment Controller", func() {
	ctx := context.Background()

	const infraName = "att-infra"

	createInfraForAttachment := func() {
		Expect(k8sClient.Create(ctx, &hostedclusterv1alpha1.Infra{
			ObjectMeta: metav1.ObjectMeta{Name: infraName, Namespace: "default"},
			Spec: hostedclusterv1alpha1.InfraSpec{
				NetworkConfig: hostedclusterv1alpha1.NetworkConfig{
					CIDR:                        "192.168.100.0/24",
					Gateway:                     "192.168.100.1",
					NetworkAttachmentDefinition: "vlan100",
				},
			},
		})).To(Succeed())
	}

	reconcileAttachment := func(r *InfraClusterAttachmentReconciler, name string) {
		_, err := r.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: name, Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())
	}

	AfterEach(func() {
		var atts hostedclusterv1alpha1.InfraClusterAttachmentList
		Expect(k8sClient.List(ctx, &atts, client.InNamespace("default"))).To(Succeed())
		for i := range atts.Items {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &atts.Items[i]))).To(Succeed())
		}
		infra := &hostedclusterv1alpha1.Infra{ObjectMeta: metav1.ObjectMeta{Name: infraName, Namespace: "default"}}
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, infra))).To(Succeed())
	})

	getAttachment := func(name string) hostedclusterv1alpha1.InfraClusterAttachment {
		var att hostedclusterv1alpha1.InfraClusterAttachment
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, &att)).To(Succeed())
		return att
	}

	It("derives defaults, adds a finalizer, and reports Ready", func() {
		createInfraForAttachment()
		ensureNamespace(ctx, "clusters-defaults")

		att := makeAttachment("defaults", infraName, "example-hcp", "example.com", "")
		att.Spec.ControlPlaneNamespace = "" // exercise derivation
		Expect(k8sClient.Create(ctx, att)).To(Succeed())

		r := &InfraClusterAttachmentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		reconcileAttachment(r, "defaults") // finalizer add pass
		reconcileAttachment(r, "defaults") // main pass

		got := getAttachment("defaults")
		Expect(got.Status.Domain).To(Equal("example-hcp.example.com"))
		Expect(got.Status.ControlPlaneNamespace).To(Equal("clusters-defaults"))
		cond := meta.FindStatusCondition(got.Status.Conditions, "Ready")
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionTrue))

		var policy networkingv1.NetworkPolicy
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: "allow-infrastructure", Namespace: "clusters-defaults",
		}, &policy)).To(Succeed())

		Expect(k8sClient.Delete(ctx, &got)).To(Succeed())
		reconcileAttachment(r, "defaults") // cleanup pass removes finalizer

		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: "defaults", Namespace: "default"}, &hostedclusterv1alpha1.InfraClusterAttachment{})
			return errors.IsNotFound(err)
		}).Should(BeTrue())
	})

	It("reports InfraNotFound when the referenced Infra is missing", func() {
		att := makeAttachment("orphan", "missing-infra", "orphan", "example.com", "")
		Expect(k8sClient.Create(ctx, att)).To(Succeed())

		r := &InfraClusterAttachmentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		reconcileAttachment(r, "orphan")
		reconcileAttachment(r, "orphan")

		cond := meta.FindStatusCondition(getAttachment("orphan").Status.Conditions, "Ready")
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(reasonAttachmentInfraNotFound))
	})

	It("rejects apps ingress without MetalLB settings", func() {
		createInfraForAttachment()
		att := makeAttachment("badapps", infraName, "badapps", "example.com", "")
		att.Spec.AppsIngress = hostedclusterv1alpha1.AppsIngressConfig{Enabled: true}
		Expect(k8sClient.Create(ctx, att)).To(Succeed())

		r := &InfraClusterAttachmentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		reconcileAttachment(r, "badapps")
		reconcileAttachment(r, "badapps")

		got := getAttachment("badapps")
		Expect(got.Status.AppsIngressStatus.Phase).To(Equal(PhaseDegraded))
		Expect(got.Status.AppsIngressStatus.Reason).To(Equal(reasonAttachmentInvalidConfig))
		cond := meta.FindStatusCondition(got.Status.Conditions, "Ready")
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
	})
})
