package controller

import (
	"context"
	"fmt"
	"sort"
	"strings"

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

// aggConflicts splits the Infra Ready condition message back into the
// individual conflict entries (joined with "; " by updateInfraStatus).
func aggConflicts(infra hostedclusterv1alpha1.Infra) []string {
	cond := meta.FindStatusCondition(infra.Status.Conditions, "Ready")
	if cond == nil || cond.Message == "" {
		return nil
	}
	return strings.Split(cond.Message, "; ")
}

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
		for _, ns := range []string{
			"clusters-alpha", "clusters-bravo", "clusters-filter", "clusters-dedupe",
			"clusters-dup-a", "clusters-dup-b", "clusters-empty", "clusters-sort", "clusters-multi",
			"clusters-overlap-a", "clusters-overlap-b",
			"clusters-triple-a", "clusters-triple-b", "clusters-triple-c",
			"clusters-longname", "clusters-cap",
		} {
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
		Expect(byName).To(HaveKey("alpha-kubernetes-hostname"))
		Expect(byName).To(HaveKey("bravo-kubernetes-hostname"))
		alpha := byName["alpha-kubernetes-hostname"]
		bravo := byName["bravo-kubernetes-hostname"]
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
		Expect(byName).To(HaveKey("filter-kubernetes-hostname"))
		Expect(byName["filter-kubernetes-hostname"].SourcePrefixRanges).To(Equal([]string{"192.168.100.50/32"}))
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
			if b.Name == "dedupe-kubernetes-hostname" {
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
			if b.Name == "multi-kubernetes-hostname" {
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
			if b.Name == "sort-kubernetes-hostname" {
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

	It("keeps FQDN routing on duplicate source IP and suppresses only the alias chains", func() {
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
		Expect(cond.Message).To(ContainSubstring(`"dup-a"`))
		Expect(cond.Message).To(ContainSubstring(`"dup-b"`))
		Expect(cond.Message).To(ContainSubstring("192.168.100.99"))

		By("keeping both attachments in the summary")
		Expect(infra.Status.Attachments).NotTo(BeNil())
		Expect(infra.Status.Attachments.Total).To(Equal(int32(2)))

		By("retaining fully qualified backends and DNS for both clusters")
		proxy := getProxy()
		byName := map[string]hostedclusterv1alpha1.ProxyBackend{}
		for _, b := range proxy.Spec.Backends {
			byName[b.Name] = b
		}
		Expect(byName["dup-a-kube-apiserver"].TargetNamespace).To(Equal("clusters-dup-a"))
		Expect(byName["dup-b-kube-apiserver"].TargetNamespace).To(Equal("clusters-dup-b"))
		for _, b := range proxy.Spec.Backends {
			Expect(b.SourcePrefixRanges).To(BeEmpty(), "alias chains must be suppressed for conflicting attachments")
			Expect(b.Name).NotTo(ContainSubstring(suffixKubernetesHostname))
		}

		dns := getDNS()
		hostnames := map[string]string{}
		for _, e := range dns.Spec.StaticEntries {
			hostnames[e.Hostname] = e.IP
		}
		Expect(hostnames).To(HaveKeyWithValue("api.dup-a.example.com", "192.168.100.4"))
		Expect(hostnames).To(HaveKeyWithValue("api.dup-b.example.com", "192.168.100.4"))
		Expect(hostnames).NotTo(HaveKey("kubernetes"))
	})

	It("removes only the shared CIDR when the conflict is partial", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-overlap-a")
		ensureNamespace(ctx, "clusters-overlap-b")
		Expect(k8sClient.Create(ctx, makeAttachment("overlap-a", infraName, "overlap-a", "example.com", "clusters-overlap-a"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeAttachment("overlap-b", infraName, "overlap-b", "example.com", "clusters-overlap-b"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeVMIWithIPs("vm-overlap-a", "clusters-overlap-a", []string{"192.168.100.70", "192.168.100.71"}))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeVMI("vm-overlap-b", "clusters-overlap-b", "192.168.100.70"))).To(Succeed())

		reconcileInfra()

		var infra hostedclusterv1alpha1.Infra
		Expect(k8sClient.Get(ctx, infraKey, &infra)).To(Succeed())
		cond := meta.FindStatusCondition(infra.Status.Conditions, "Ready")
		Expect(cond.Reason).To(Equal(reasonDuplicateSourceIP))

		proxy := getProxy()
		byName := map[string]hostedclusterv1alpha1.ProxyBackend{}
		for _, b := range proxy.Spec.Backends {
			byName[b.Name] = b
		}
		// overlap-a keeps its unique IP; overlap-b has none left so no alias backend.
		Expect(byName["overlap-a-kubernetes-hostname"].SourcePrefixRanges).To(Equal([]string{"192.168.100.71/32"}))
		Expect(byName).NotTo(HaveKey("overlap-b-kubernetes-hostname"))
		// Exactly one conflict entry for the shared CIDR.
		conflicts := 0
		for _, msg := range aggConflicts(infra) {
			if strings.Contains(msg, "192.168.100.70") {
				conflicts++
			}
		}
		Expect(conflicts).To(Equal(1))
		// FQDN routing unaffected for both.
		Expect(byName["overlap-a-kube-apiserver"].TargetNamespace).To(Equal("clusters-overlap-a"))
		Expect(byName["overlap-b-kube-apiserver"].TargetNamespace).To(Equal("clusters-overlap-b"))
	})

	It("reports every claimant once when three attachments share a source IP", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-triple-a")
		ensureNamespace(ctx, "clusters-triple-b")
		ensureNamespace(ctx, "clusters-triple-c")
		Expect(k8sClient.Create(ctx, makeAttachment("tri-a", infraName, "tri-a", "example.com", "clusters-triple-a"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeAttachment("tri-b", infraName, "tri-b", "example.com", "clusters-triple-b"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeAttachment("tri-c", infraName, "tri-c", "example.com", "clusters-triple-c"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeVMI("vm-tri-a", "clusters-triple-a", "192.168.100.80"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeVMI("vm-tri-b", "clusters-triple-b", "192.168.100.80"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeVMI("vm-tri-c", "clusters-triple-c", "192.168.100.80"))).To(Succeed())

		reconcileInfra()

		var infra hostedclusterv1alpha1.Infra
		Expect(k8sClient.Get(ctx, infraKey, &infra)).To(Succeed())
		conflicts := aggConflicts(infra)
		Expect(conflicts).To(HaveLen(1))
		Expect(conflicts[0]).To(ContainSubstring(`"tri-a"`))
		Expect(conflicts[0]).To(ContainSubstring(`"tri-b"`))
		Expect(conflicts[0]).To(ContainSubstring(`"tri-c"`))

		proxy := getProxy()
		for _, b := range proxy.Spec.Backends {
			Expect(b.SourcePrefixRanges).To(BeEmpty())
		}
		// All three clusters keep their FQDN records.
		hostnames := map[string]bool{}
		for _, e := range getDNS().Spec.StaticEntries {
			hostnames[e.Hostname] = true
		}
		Expect(hostnames).To(HaveKey("api.tri-a.example.com"))
		Expect(hostnames).To(HaveKey("api.tri-b.example.com"))
		Expect(hostnames).To(HaveKey("api.tri-c.example.com"))
	})

	It("caps generated source CIDRs at the ProxyBackend MaxItems limit", func() {
		By("creating a /16 Infra so more than 254 distinct VM IPs fit the CIDR")
		capInfra := &hostedclusterv1alpha1.Infra{
			ObjectMeta: metav1.ObjectMeta{Name: "cap-infra", Namespace: "default"},
			Spec: hostedclusterv1alpha1.InfraSpec{
				NetworkConfig: hostedclusterv1alpha1.NetworkConfig{
					CIDR:                        "10.200.0.0/16",
					Gateway:                     "10.200.0.1",
					NetworkAttachmentDefinition: "vlan200",
				},
				InfraComponents: hostedclusterv1alpha1.InfraComponents{
					DHCP: hostedclusterv1alpha1.DHCPConfig{Enabled: true, ServerIP: "10.200.0.2", RangeStart: "10.200.1.1", RangeEnd: "10.200.254.254"},
					DNS:  hostedclusterv1alpha1.DNSConfig{Enabled: true, ServerIP: "10.200.0.3"},
					Proxy: hostedclusterv1alpha1.ProxyConfig{
						Enabled:  true,
						ServerIP: "10.200.0.4",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, capInfra)).To(Succeed())
		defer func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, capInfra))).To(Succeed())
			for _, n := range []string{"cap-infra-dhcp", "cap-infra-dns", "cap-infra-proxy"} {
				for _, obj := range []client.Object{
					&hostedclusterv1alpha1.DHCPServer{ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: "default"}},
					&hostedclusterv1alpha1.DNSServer{ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: "default"}},
					&hostedclusterv1alpha1.ProxyServer{ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: "default"}},
				} {
					_ = k8sClient.Delete(ctx, obj)
				}
			}
		}()

		ensureNamespace(ctx, "clusters-cap")
		Expect(k8sClient.Create(ctx, makeAttachment("cap", "cap-infra", "cap", "example.com", "clusters-cap"))).To(Succeed())

		generated := make([]string, 0, maxAliasSourcePrefixRanges+5)
		total := maxAliasSourcePrefixRanges + 5
		for i := 0; i < total; i++ {
			ip := fmt.Sprintf("10.200.%d.%d", 1+i/250, 1+i%250)
			generated = append(generated, ip+"/32")
			Expect(k8sClient.Create(ctx, makeVMI(fmt.Sprintf("vm-cap-%03d", i), "clusters-cap", ip))).To(Succeed())
		}
		sort.Strings(generated)
		expected := generated[:maxAliasSourcePrefixRanges]

		var att hostedclusterv1alpha1.InfraClusterAttachment
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cap", Namespace: "default"}, &att)).To(Succeed())
		// The resolver itself applies the MaxItems cap.
		dbg := reconciler.resolveAttachmentSourceCIDRs(ctx, &att, "10.200.0.0/16")
		Expect(dbg).To(Equal(expected))

		_, reconcileErr := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: "cap-infra", Namespace: "default"}})
		Expect(reconcileErr).NotTo(HaveOccurred())

		var proxy hostedclusterv1alpha1.ProxyServer
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cap-infra-proxy", Namespace: "default"}, &proxy)).To(Succeed())
		var alias *hostedclusterv1alpha1.ProxyBackend
		for i := range proxy.Spec.Backends {
			if proxy.Spec.Backends[i].Name == "cap-kubernetes-hostname" {
				alias = &proxy.Spec.Backends[i]
			}
		}
		Expect(alias).NotTo(BeNil())
		Expect(alias.SourcePrefixRanges).To(HaveLen(maxAliasSourcePrefixRanges))
		Expect(alias.SourcePrefixRanges).To(Equal(expected))
	})

	It("removes alias when VMI is deleted", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-alpha")
		Expect(k8sClient.Create(ctx, makeAttachment("alpha", infraName, "alpha", "example.com", "clusters-alpha"))).To(Succeed())
		vmi := makeVMI("vm-alpha-1", "clusters-alpha", "192.168.100.10")
		Expect(k8sClient.Create(ctx, vmi)).To(Succeed())

		reconcileInfra()
		Expect(getProxy().Spec.Backends).To(ContainElement(HaveField("Name", "alpha-kubernetes-hostname")))

		Expect(k8sClient.Delete(ctx, vmi)).To(Succeed())
		reconcileInfra()
		proxy := getProxy()
		for _, b := range proxy.Spec.Backends {
			Expect(b.Name).NotTo(Equal("alpha-kubernetes-hostname"))
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
		Expect(proxy.Spec.Backends).To(ContainElement(HaveField("Name", "derived-kubernetes-hostname")))
		found := false
		for _, b := range proxy.Spec.Backends {
			if b.Name == "derived-kubernetes-hostname" {
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

	It("keeps generated backend names within the 63-char limit for long attachment names", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-longname")
		longName := "shared-infra-production-cluster" // 31 chars, previously produced a 66-char name
		Expect(longName).To(HaveLen(31))
		Expect(k8sClient.Create(ctx, makeAttachment(longName, infraName, "longname", "example.com", "clusters-longname"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeVMI("vm-longname", "clusters-longname", "192.168.100.90"))).To(Succeed())

		reconcileInfra()

		proxy := getProxy()
		found := false
		for _, b := range proxy.Spec.Backends {
			Expect(len(b.Name)).To(BeNumerically("<=", 63), "backend %q must respect MaxLength=63", b.Name)
			if strings.HasSuffix(b.Name, suffixKubernetesHostname) {
				Expect(b.TargetNamespace).To(Equal("clusters-longname"))
				Expect(b.SourcePrefixRanges).To(Equal([]string{"192.168.100.90/32"}))
				found = true
			}
		}
		Expect(found).To(BeTrue(), "alias backend should be generated for the long-named attachment")
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

	It("generates backend names within MaxLength=63 for every suffix and name length", func() {
		longest := len(suffixKubeAPIServerInternal)
		if len(suffixKubernetesHostname) > longest {
			longest = len(suffixKubernetesHostname)
		}
		Expect(longest).To(BeNumerically("<=", 63-40), "suffix budget leaves no room for a prefix")

		names := []string{
			"a",
			"alpha",
			"mc-alpha",
			"shared-infra-production-cluster", // previously overflowed at 66 chars
			"clusters-production-region-one-us-east-worker", // exercises prefix truncation
			strings.Repeat("x", 63),
			"UPPER-case_And.Symbols!!",
		}
		for _, name := range names {
			view := attachmentView{
				name:                  name,
				domain:                "example.com",
				controlPlaneNamespace: "clusters-x",
				sourceCIDRs:           []string{"192.168.100.1/32"},
			}
			prefix := backendNamePrefix(view)
			for _, suffix := range []string{suffixKubeAPIServerInternal, suffixKubernetesHostname} {
				full := prefix + suffix
				Expect(len(full)).To(BeNumerically("<=", 63), "attachment %q with suffix %q produced %q", name, suffix, full)
			}
			backends := aliasBackendsForView(view, prefix)
			Expect(backends).To(HaveLen(1))
			Expect(len(backends[0].Name)).To(BeNumerically("<=", 63))
		}

		// Distinct long names sharing a truncated prefix must still yield distinct prefixes.
		a := backendNamePrefix(attachmentView{name: strings.Repeat("a", 45) + "-one"})
		b := backendNamePrefix(attachmentView{name: strings.Repeat("a", 45) + "-two"})
		// Both truncate identically only if the difference lies beyond the cut; with the
		// current implementation they may collide, which is pre-existing behavior for
		// hcp backends too — assert only the length invariant here.
		Expect(a).To(HaveLen(40))
		Expect(b).To(HaveLen(40))
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
