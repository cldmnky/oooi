package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	hostedclusterv1alpha1 "github.com/cldmnky/oooi/api/v1alpha1"
	kubevirtv1 "kubevirt.io/api/core/v1"
)

func makeVMI(name, namespace, ip string) *kubevirtv1.VirtualMachineInstance {
	return &kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Status: kubevirtv1.VirtualMachineInstanceStatus{
			Interfaces: []kubevirtv1.VirtualMachineInstanceNetworkInterface{
				{IP: ip, IPs: []string{ip}},
			},
		},
	}
}

func makeVMIWithIPs(name, namespace string, ips []string) *kubevirtv1.VirtualMachineInstance {
	if len(ips) == 0 {
		return &kubevirtv1.VirtualMachineInstance{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Status: kubevirtv1.VirtualMachineInstanceStatus{
				Interfaces: []kubevirtv1.VirtualMachineInstanceNetworkInterface{{Name: "default"}},
			},
		}
	}
	iface := kubevirtv1.VirtualMachineInstanceNetworkInterface{
		Name: "default",
		IP:   ips[0],
		IPs:  ips,
	}
	return &kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Status: kubevirtv1.VirtualMachineInstanceStatus{
			Interfaces: []kubevirtv1.VirtualMachineInstanceNetworkInterface{iface},
		},
	}
}

var _ = Describe("Infra source-IP alias aggregation", func() {
	ctx := context.Background()
	var reconciler *InfraReconciler
	const infraName = "alias-infra"
	infraKey := types.NamespacedName{Name: infraName, Namespace: "default"}

	createAliasInfra := func() {
		spec := hostedclusterv1alpha1.InfraSpec{
			NetworkConfig: hostedclusterv1alpha1.NetworkConfig{
				CIDR:                        "192.168.100.0/24",
				Gateway:                     "192.168.100.1",
				NetworkAttachmentDefinition: "vlan100",
			},
			InfraComponents: hostedclusterv1alpha1.InfraComponents{
				DHCP: hostedclusterv1alpha1.DHCPConfig{Enabled: true, ServerIP: "192.168.100.2", RangeStart: "192.168.100.10", RangeEnd: "192.168.100.200"},
				DNS:  hostedclusterv1alpha1.DNSConfig{Enabled: true, ServerIP: "192.168.100.3"},
				Proxy: hostedclusterv1alpha1.ProxyConfig{
					Enabled:  true,
					ServerIP: "192.168.100.4",
				},
			},
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

	getProxy := func() hostedclusterv1alpha1.ProxyServer {
		var proxy hostedclusterv1alpha1.ProxyServer
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: infraName + "-proxy", Namespace: "default"}, &proxy)).To(Succeed())
		return proxy
	}

	getDNS := func() hostedclusterv1alpha1.DNSServer {
		var dns hostedclusterv1alpha1.DNSServer
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: infraName + "-dns", Namespace: "default"}, &dns)).To(Succeed())
		return dns
	}

	BeforeEach(func() {
		reconciler = &InfraReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
	})

	AfterEach(func() {
		var atts hostedclusterv1alpha1.InfraClusterAttachmentList
		Expect(k8sClient.List(ctx, &atts, client.InNamespace("default"))).To(Succeed())
		for i := range atts.Items {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &atts.Items[i]))).To(Succeed())
		}
		infra := &hostedclusterv1alpha1.Infra{ObjectMeta: metav1.ObjectMeta{Name: infraName, Namespace: "default"}}
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, infra))).To(Succeed())
		for _, n := range []string{infraName + "-dhcp", infraName + "-dns", infraName + "-proxy"} {
			for _, obj := range []client.Object{
				&hostedclusterv1alpha1.DHCPServer{ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: "default"}},
				&hostedclusterv1alpha1.DNSServer{ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: "default"}},
				&hostedclusterv1alpha1.ProxyServer{ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: "default"}},
			} {
				_ = k8sClient.Delete(ctx, obj)
			}
		}
		// Clean VMIs and namespaces created in these tests
		for _, ns := range []string{"clusters-alpha", "clusters-bravo", "clusters-filter", "clusters-dedupe", "clusters-dup-a", "clusters-dup-b", "clusters-empty", "clusters-sort", "clusters-multi"} {
			vlist := &kubevirtv1.VirtualMachineInstanceList{}
			_ = k8sClient.List(ctx, vlist, client.InNamespace(ns))
			for i := range vlist.Items {
				_ = k8sClient.Delete(ctx, &vlist.Items[i])
			}
		}
	})

	It("creates source-IP scoped kubernetes alias backends when VMIs are present", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-alpha")
		ensureNamespace(ctx, "clusters-bravo")
		Expect(k8sClient.Create(ctx, makeAttachment("alpha", infraName, "alpha", "example.com", "clusters-alpha"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeAttachment("bravo", infraName, "bravo", "example.com", "clusters-bravo"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeVMI("vm-alpha-1", "clusters-alpha", "192.168.100.10"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeVMI("vm-bravo-1", "clusters-bravo", "192.168.100.11"))).To(Succeed())

		reconcileInfra()

		proxy := getProxy()
		byName := map[string]hostedclusterv1alpha1.ProxyBackend{}
		for _, b := range proxy.Spec.Backends {
			byName[b.Name] = b
		}
		Expect(byName).To(HaveKey("alpha-kube-apiserver-kubernetes-hostname"))
		Expect(byName).To(HaveKey("bravo-kube-apiserver-kubernetes-hostname"))
		alpha := byName["alpha-kube-apiserver-kubernetes-hostname"]
		bravo := byName["bravo-kube-apiserver-kubernetes-hostname"]
		Expect(alpha.Hostname).To(Equal("kubernetes"))
		Expect(alpha.AlternateHostnames).To(ContainElements("kubernetes.default", "kubernetes.default.svc", "kubernetes.default.svc.cluster.local", "kubernetes.alpha.example.com"))
		Expect(alpha.SourcePrefixRanges).To(Equal([]string{"192.168.100.10/32"}))
		Expect(alpha.Port).To(Equal(int32(443)))
		Expect(alpha.TargetNamespace).To(Equal("clusters-alpha"))
		Expect(bravo.SourcePrefixRanges).To(Equal([]string{"192.168.100.11/32"}))
		Expect(bravo.TargetNamespace).To(Equal("clusters-bravo"))

		By("adding kubernetes DNS entries once")
		dns := getDNS()
		hostnames := map[string]string{}
		for _, e := range dns.Spec.StaticEntries {
			hostnames[e.Hostname] = e.IP
		}
		Expect(hostnames).To(HaveKeyWithValue("kubernetes", "192.168.100.4"))
		Expect(hostnames).To(HaveKeyWithValue("kubernetes.default", "192.168.100.4"))
		Expect(hostnames).To(HaveKeyWithValue("kubernetes.default.svc", "192.168.100.4"))
		Expect(hostnames).To(HaveKeyWithValue("kubernetes.default.svc.cluster.local", "192.168.100.4"))
		// Ensure FQDN still present
		Expect(hostnames).To(HaveKey("api.alpha.example.com"))

		By("keeping FQDN backends distinct")
		Expect(byName).To(HaveKey("alpha-kube-apiserver"))
		Expect(byName).To(HaveKey("bravo-kube-apiserver"))
		Expect(byName["alpha-kube-apiserver"].TargetNamespace).To(Equal("clusters-alpha"))
		Expect(byName["bravo-kube-apiserver"].TargetNamespace).To(Equal("clusters-bravo"))
	})

	It("filters VM IPs outside the Infra CIDR", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-filter")
		Expect(k8sClient.Create(ctx, makeAttachment("filter", infraName, "filter", "example.com", "clusters-filter"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeVMI("vm-filter-1", "clusters-filter", "10.0.0.5"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeVMI("vm-filter-2", "clusters-filter", "192.168.100.50"))).To(Succeed())

		reconcileInfra()
		proxy := getProxy()
		byName := map[string]hostedclusterv1alpha1.ProxyBackend{}
		for _, b := range proxy.Spec.Backends {
			byName[b.Name] = b
		}
		Expect(byName).To(HaveKey("filter-kube-apiserver-kubernetes-hostname"))
		Expect(byName["filter-kube-apiserver-kubernetes-hostname"].SourcePrefixRanges).To(Equal([]string{"192.168.100.50/32"}))
	})

	It("deduplicates IPs within same attachment", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-dedupe")
		Expect(k8sClient.Create(ctx, makeAttachment("dedupe", infraName, "dedupe", "example.com", "clusters-dedupe"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeVMI("vm-dedupe-1", "clusters-dedupe", "192.168.100.20"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeVMI("vm-dedupe-2", "clusters-dedupe", "192.168.100.20"))).To(Succeed())

		reconcileInfra()
		proxy := getProxy()
		for _, b := range proxy.Spec.Backends {
			if b.Name == "dedupe-kube-apiserver-kubernetes-hostname" {
				Expect(b.SourcePrefixRanges).To(Equal([]string{"192.168.100.20/32"}))
			}
		}
	})

	It("handles multiple interfaces and IPs arrays per VMI", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-multi")
		Expect(k8sClient.Create(ctx, makeAttachment("multi", infraName, "multi", "example.com", "clusters-multi"))).To(Succeed())
		vmi := &kubevirtv1.VirtualMachineInstance{
			ObjectMeta: metav1.ObjectMeta{Name: "vm-multi", Namespace: "clusters-multi"},
			Status: kubevirtv1.VirtualMachineInstanceStatus{
				Interfaces: []kubevirtv1.VirtualMachineInstanceNetworkInterface{
					{Name: "default", IP: "192.168.100.30", IPs: []string{"192.168.100.30", "192.168.100.31"}},
					{Name: "extra", IP: "192.168.100.32", IPs: []string{"192.168.100.32"}},
				},
			},
		}
		Expect(k8sClient.Create(ctx, vmi)).To(Succeed())

		reconcileInfra()
		proxy := getProxy()
		for _, b := range proxy.Spec.Backends {
			if b.Name == "multi-kube-apiserver-kubernetes-hostname" {
				Expect(b.SourcePrefixRanges).To(Equal([]string{"192.168.100.30/32", "192.168.100.31/32", "192.168.100.32/32"}))
			}
		}
	})

	It("sorts source CIDRs deterministically", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-sort")
		Expect(k8sClient.Create(ctx, makeAttachment("sort", infraName, "sort", "example.com", "clusters-sort"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeVMIWithIPs("vm-sort", "clusters-sort", []string{"192.168.100.52", "192.168.100.50", "192.168.100.51"}))).To(Succeed())

		reconcileInfra()
		proxy := getProxy()
		for _, b := range proxy.Spec.Backends {
			if b.Name == "sort-kube-apiserver-kubernetes-hostname" {
				Expect(b.SourcePrefixRanges).To(Equal([]string{"192.168.100.50/32", "192.168.100.51/32", "192.168.100.52/32"}))
			}
		}
	})

	It("omits alias backends when no VMIs exist", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-empty")
		Expect(k8sClient.Create(ctx, makeAttachment("empty", infraName, "empty", "example.com", "clusters-empty"))).To(Succeed())

		reconcileInfra()
		proxy := getProxy()
		for _, b := range proxy.Spec.Backends {
			Expect(b.Name).NotTo(ContainSubstring("kubernetes-hostname"))
			Expect(b.SourcePrefixRanges).To(BeEmpty())
		}
		dns := getDNS()
		for _, e := range dns.Spec.StaticEntries {
			Expect(e.Hostname).NotTo(Equal("kubernetes"))
			Expect(e.Hostname).NotTo(Equal("kubernetes.default"))
		}
	})

	It("omits alias when VMI has no IPs", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-empty")
		Expect(k8sClient.Create(ctx, makeAttachment("noip", infraName, "noip", "example.com", "clusters-empty"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeVMIWithIPs("vm-noip", "clusters-empty", nil))).To(Succeed())

		reconcileInfra()
		proxy := getProxy()
		for _, b := range proxy.Spec.Backends {
			Expect(b.Name).NotTo(ContainSubstring("kubernetes-hostname"))
		}
	})

	It("excludes both attachments on duplicate source IP and marks Degraded", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-dup-a")
		ensureNamespace(ctx, "clusters-dup-b")
		Expect(k8sClient.Create(ctx, makeAttachment("dup-a", infraName, "dup-a", "example.com", "clusters-dup-a"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeAttachment("dup-b", infraName, "dup-b", "example.com", "clusters-dup-b"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeVMI("vm-dup-a", "clusters-dup-a", "192.168.100.99"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeVMI("vm-dup-b", "clusters-dup-b", "192.168.100.99"))).To(Succeed())

		reconcileInfra()

		var infra hostedclusterv1alpha1.Infra
		Expect(k8sClient.Get(ctx, infraKey, &infra)).To(Succeed())
		cond := meta.FindStatusCondition(infra.Status.Conditions, "Ready")
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(reasonDuplicateSourceIP))
		Expect(cond.Message).To(ContainSubstring("192.168.100.99"))

		// No proxy/dns children when all attachments conflict (both excluded)
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: infraName + "-proxy", Namespace: "default"}, &hostedclusterv1alpha1.ProxyServer{})).To(MatchError(ContainSubstring("not found")))
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: infraName + "-dns", Namespace: "default"}, &hostedclusterv1alpha1.DNSServer{})).To(MatchError(ContainSubstring("not found")))
	})

	It("removes alias when VMI is deleted", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-alpha")
		Expect(k8sClient.Create(ctx, makeAttachment("alpha", infraName, "alpha", "example.com", "clusters-alpha"))).To(Succeed())
		vmi := makeVMI("vm-alpha-1", "clusters-alpha", "192.168.100.10")
		Expect(k8sClient.Create(ctx, vmi)).To(Succeed())

		reconcileInfra()
		Expect(getProxy().Spec.Backends).To(ContainElement(HaveField("Name", "alpha-kube-apiserver-kubernetes-hostname")))

		Expect(k8sClient.Delete(ctx, vmi)).To(Succeed())
		reconcileInfra()
		proxy := getProxy()
		for _, b := range proxy.Spec.Backends {
			Expect(b.Name).NotTo(Equal("alpha-kube-apiserver-kubernetes-hostname"))
		}
		dns := getDNS()
		for _, e := range dns.Spec.StaticEntries {
			Expect(e.Hostname).NotTo(Equal("kubernetes"))
		}
	})

	It("derives control-plane namespace when InfraClusterAttachment omits it", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-derived")
		att := makeAttachment("derived", infraName, "derived", "example.com", "")
		att.Spec.ControlPlaneNamespace = ""
		att.Spec.HostedClusterRef.Namespace = "clusters"
		att.Spec.HostedClusterRef.Name = "derived"
		Expect(k8sClient.Create(ctx, att)).To(Succeed())
		Expect(k8sClient.Create(ctx, makeVMI("vm-derived", "clusters-derived", "192.168.100.77"))).To(Succeed())

		reconcileInfra()
		proxy := getProxy()
		Expect(proxy.Spec.Backends).To(ContainElement(HaveField("Name", "derived-kube-apiserver-kubernetes-hostname")))
		found := false
		for _, b := range proxy.Spec.Backends {
			if b.Name == "derived-kube-apiserver-kubernetes-hostname" {
				Expect(b.TargetNamespace).To(Equal("clusters-derived"))
				Expect(b.SourcePrefixRanges).To(Equal([]string{"192.168.100.77/32"}))
				found = true
			}
		}
		Expect(found).To(BeTrue())
	})

	It("adds kubernetes DNS entries only once with multiple aliases", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-alpha")
		ensureNamespace(ctx, "clusters-bravo")
		Expect(k8sClient.Create(ctx, makeAttachment("alpha", infraName, "alpha", "example.com", "clusters-alpha"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeAttachment("bravo", infraName, "bravo", "example.com", "clusters-bravo"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeVMI("vm-alpha-1", "clusters-alpha", "192.168.100.10"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeVMI("vm-bravo-1", "clusters-bravo", "192.168.100.11"))).To(Succeed())

		reconcileInfra()
		dns := getDNS()
		count := map[string]int{}
		for _, e := range dns.Spec.StaticEntries {
			count[e.Hostname]++
		}
		Expect(count["kubernetes"]).To(Equal(1))
		Expect(count["kubernetes.default"]).To(Equal(1))
		Expect(count["kubernetes.default.svc"]).To(Equal(1))
		Expect(count["kubernetes.default.svc.cluster.local"]).To(Equal(1))
	})

	It("excludes terminating attachments from alias aggregation", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-alpha")
		Expect(k8sClient.Create(ctx, makeAttachment("active", infraName, "active", "example.com", "clusters-alpha"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeVMI("vm-active", "clusters-alpha", "192.168.100.10"))).To(Succeed())
		terminating := makeAttachment("terminating", infraName, "terminating", "example.com", "clusters-alpha")
		terminating.Finalizers = []string{"test.finalizer"}
		Expect(k8sClient.Create(ctx, terminating)).To(Succeed())
		Expect(k8sClient.Delete(ctx, terminating)).To(Succeed())

		reconcileInfra()
		proxy := getProxy()
		// Terminating attachment should not contribute its domain or alias (its VMs are in same ns but excluded)
		for _, b := range proxy.Spec.Backends {
			Expect(b.Hostname).NotTo(ContainSubstring("terminating"))
		}
		var infra hostedclusterv1alpha1.Infra
		Expect(k8sClient.Get(ctx, infraKey, &infra)).To(Succeed())
		Expect(infra.Status.Attachments.Total).To(Equal(int32(1)))
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "terminating", Namespace: "default"}, terminating)).To(Succeed())
		terminating.Finalizers = nil
		Expect(k8sClient.Update(ctx, terminating)).To(Succeed())
	})

	It("reports ValidDomain handling even with alias", func() {
		// Empty clusterName is rejected by CRD validation, so test via helper directly.
		invalid := attachmentView{name: "bad", domain: "", sourceCIDRs: []string{"192.168.100.10/32"}}
		Expect(aliasBackendsForView(invalid, "bad-")).To(BeNil())
		Expect(validDomain("")).To(BeFalse())
		Expect(validDomain(".")).To(BeFalse())
		Expect(validDomain("bad.example.com")).To(BeTrue())
	})

	It("directly tests resolveAttachmentSourceCIDRs and aliasBackendsForView helpers", func() {
		view := attachmentView{
			name:                  "unit",
			domain:                "unit.example.com",
			controlPlaneNamespace: "clusters-alpha",
			sourceCIDRs:           []string{"192.168.100.1/32", "192.168.100.2/32"},
		}
		backends := aliasBackendsForView(view, "unit-")
		Expect(backends).To(HaveLen(1))
		Expect(backends[0].SourcePrefixRanges).To(Equal([]string{"192.168.100.1/32", "192.168.100.2/32"}))
		Expect(backends[0].AlternateHostnames).To(ContainElement("kubernetes.default"))

		empty := attachmentView{name: "empty", domain: "empty.example.com"}
		Expect(aliasBackendsForView(empty, "empty-")).To(BeNil())

		invalid := attachmentView{name: "bad", domain: ".", sourceCIDRs: []string{"192.168.100.1/32"}}
		Expect(aliasBackendsForView(invalid, "bad-")).To(BeNil())
	})

	It("keeps FQDN backends SNI-routed even without alias", func() {
		createAliasInfra()
		Expect(k8sClient.Create(ctx, makeAttachment("alpha", infraName, "alpha", "example.com", "clusters-alpha"))).To(Succeed())
		reconcileInfra()
		proxy := getProxy()
		Expect(proxy.Spec.Backends).To(ContainElement(HaveField("Hostname", "api.alpha.example.com")))
		Expect(proxy.Spec.Backends).To(ContainElement(HaveField("Hostname", "oauth.alpha.example.com")))
	})
})

// Additional exhaustive checks for 6443 SNI handling are covered in proxy server_test.go
var _ = Describe("helper coverage", func() {
	It("validates domain helper edge cases", func() {
		Expect(validDomain("a.b")).To(BeTrue())
		Expect(validDomain("")).To(BeFalse())
		Expect(validDomain(".")).To(BeFalse())
		Expect(validDomain("alpha.example.com")).To(BeTrue())
	})
	It("checks hostnameKey and merge helpers via proxy status", func() {
		// Indirectly tested via shared external Service test; ensure no panic on empty.
		Expect(mergeOAuthHostnames(nil, nil)).To(Not(BeNil()))
		Expect(hostnameKey("  OAuth.Example.Com. ")).To(Equal("oauth.example.com"))
	})
})

var _ = Describe("resolveAttachmentSourceCIDRs edge cases", func() {
	It("returns nil for empty CIDR and invalid CIDR", func() {
		r := &InfraReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		att := makeAttachment("x", "y", "x", "example.com", "clusters-x")
		Expect(r.resolveAttachmentSourceCIDRs(context.Background(), att, "")).To(BeNil())
		Expect(r.resolveAttachmentSourceCIDRs(context.Background(), att, "not-a-cidr")).To(BeNil())
	})
	It("filters correctly when VMI IPs contain spaces", func() {
		ensureNamespace(context.Background(), "clusters-space")
		att := makeAttachment("space", "alias-infra", "space", "example.com", "clusters-space")
		Expect(k8sClient.Create(context.Background(), att)).To(Succeed())
		vmi := makeVMI("vm-space", "clusters-space", " 192.168.100.60 ")
		Expect(k8sClient.Create(context.Background(), vmi)).To(Succeed())
		r := &InfraReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		cidrs := r.resolveAttachmentSourceCIDRs(context.Background(), att, "192.168.100.0/24")
		Expect(cidrs).To(Equal([]string{"192.168.100.60/32"}))
		Expect(k8sClient.Delete(context.Background(), vmi)).To(Succeed())
		Expect(k8sClient.Delete(context.Background(), att)).To(Succeed())
	})
})
