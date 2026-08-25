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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	hostedclusterv1alpha1 "github.com/cldmnky/oooi/api/v1alpha1"
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

//nolint:unparam
func makeNodePool(name, namespace, clusterName string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: "hypershift.openshift.io", Version: "v1beta1", Kind: "NodePool"})
	u.SetName(name)
	u.SetNamespace(namespace)
	_ = unstructured.SetNestedField(u.Object, clusterName, "spec", "clusterName")
	_ = unstructured.SetNestedField(u.Object, "KubeVirt", "spec", "platform", "type")
	return u
}

func makeMachine(name, namespace, nodePoolKey string, ips []string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: "cluster.x-k8s.io", Version: "v1beta1", Kind: "Machine"})
	u.SetName(name)
	u.SetNamespace(namespace)
	if nodePoolKey != "" {
		ann := u.GetAnnotations()
		if ann == nil {
			ann = map[string]string{}
		}
		ann[nodePoolAnnotation] = nodePoolKey
		u.SetAnnotations(ann)
	}
	if len(ips) > 0 {
		var addrs []interface{}
		for _, ip := range ips {
			ip = strings.TrimSpace(ip)
			if ip == "" {
				continue
			}
			addrs = append(addrs, map[string]interface{}{"address": ip, "type": "InternalIP"})
		}
		if len(addrs) > 0 {
			_ = unstructured.SetNestedSlice(u.Object, addrs, "status", "addresses")
		}
	}
	return u
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

	reconcileInfra := func() reconcile.Result {
		res, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: infraKey})
		Expect(err).NotTo(HaveOccurred())
		return res
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
		ensureNamespace(ctx, "clusters")
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
		// Clean Machines and NodePools
		for _, ns := range []string{
			"clusters", "clusters-alpha", "clusters-bravo", "clusters-filter", "clusters-dedupe",
			"clusters-dup-a", "clusters-dup-b", "clusters-empty", "clusters-sort", "clusters-multi",
			"clusters-overlap-a", "clusters-overlap-b",
			"clusters-triple-a", "clusters-triple-b", "clusters-triple-c",
			"clusters-longname", "clusters-cap", "clusters-derived", "clusters-space",
		} {
			// Machines
			ml := &unstructured.UnstructuredList{}
			ml.SetGroupVersionKind(schema.GroupVersionKind{Group: "cluster.x-k8s.io", Version: "v1beta1", Kind: "MachineList"})
			_ = k8sClient.List(ctx, ml, client.InNamespace(ns))
			for i := range ml.Items {
				_ = k8sClient.Delete(ctx, &ml.Items[i])
			}
			// NodePools only in "clusters"
			if ns == "clusters" {
				nl := &unstructured.UnstructuredList{}
				nl.SetGroupVersionKind(schema.GroupVersionKind{Group: "hypershift.openshift.io", Version: "v1beta1", Kind: "NodePoolList"})
				_ = k8sClient.List(ctx, nl, client.InNamespace(ns))
				for i := range nl.Items {
					_ = k8sClient.Delete(ctx, &nl.Items[i])
				}
			}
		}
	})

	It("creates source-IP scoped kubernetes alias backends when Machines are present", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-alpha")
		ensureNamespace(ctx, "clusters-bravo")
		Expect(k8sClient.Create(ctx, makeAttachment("alpha", infraName, "alpha", "example.com", "clusters-alpha"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeAttachment("bravo", infraName, "bravo", "example.com", "clusters-bravo"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeNodePool("alpha-np", "clusters", "alpha"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeNodePool("bravo-np", "clusters", "bravo"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeMachine("machine-alpha-1", "clusters-alpha", "clusters/alpha-np", []string{"192.168.100.10"}))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeMachine("machine-bravo-1", "clusters-bravo", "clusters/bravo-np", []string{"192.168.100.11"}))).To(Succeed())

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
		Expect(hostnames).To(HaveKey("api.alpha.example.com"))

		By("keeping FQDN backends distinct")
		Expect(byName).To(HaveKey("alpha-kube-apiserver"))
		Expect(byName).To(HaveKey("bravo-kube-apiserver"))
		Expect(byName["alpha-kube-apiserver"].TargetNamespace).To(Equal("clusters-alpha"))
		Expect(byName["bravo-kube-apiserver"].TargetNamespace).To(Equal("clusters-bravo"))
	})

	It("filters Machine IPs outside the Infra CIDR", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-filter")
		Expect(k8sClient.Create(ctx, makeAttachment("filter", infraName, "filter", "example.com", "clusters-filter"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeNodePool("filter-np", "clusters", "filter"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeMachine("machine-filter-1", "clusters-filter", "clusters/filter-np", []string{"10.0.0.5"}))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeMachine("machine-filter-2", "clusters-filter", "clusters/filter-np", []string{"192.168.100.50"}))).To(Succeed())

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
		Expect(k8sClient.Create(ctx, makeNodePool("dedupe-np", "clusters", "dedupe"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeMachine("machine-dedupe-1", "clusters-dedupe", "clusters/dedupe-np", []string{"192.168.100.20"}))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeMachine("machine-dedupe-2", "clusters-dedupe", "clusters/dedupe-np", []string{"192.168.100.20"}))).To(Succeed())

		reconcileInfra()
		proxy := getProxy()
		for _, b := range proxy.Spec.Backends {
			if b.Name == "dedupe-kubernetes-hostname" {
				Expect(b.SourcePrefixRanges).To(Equal([]string{"192.168.100.20/32"}))
			}
		}
	})

	It("handles multiple addresses per Machine", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-multi")
		Expect(k8sClient.Create(ctx, makeAttachment("multi", infraName, "multi", "example.com", "clusters-multi"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeNodePool("multi-np", "clusters", "multi"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeMachine("machine-multi", "clusters-multi", "clusters/multi-np", []string{"192.168.100.30", "192.168.100.31", "192.168.100.32"}))).To(Succeed())

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
		Expect(k8sClient.Create(ctx, makeNodePool("sort-np", "clusters", "sort"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeMachine("machine-sort", "clusters-sort", "clusters/sort-np", []string{"192.168.100.52", "192.168.100.50", "192.168.100.51"}))).To(Succeed())

		reconcileInfra()
		proxy := getProxy()
		for _, b := range proxy.Spec.Backends {
			if b.Name == "sort-kubernetes-hostname" {
				Expect(b.SourcePrefixRanges).To(Equal([]string{"192.168.100.50/32", "192.168.100.51/32", "192.168.100.52/32"}))
			}
		}
	})

	It("omits alias backends when no Machines exist but requeues pending", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-empty")
		Expect(k8sClient.Create(ctx, makeAttachment("empty", infraName, "empty", "example.com", "clusters-empty"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeNodePool("empty-np", "clusters", "empty"))).To(Succeed())

		res := reconcileInfra()
		Expect(res.RequeueAfter).To(Equal(aliasPendingRequeue))
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

	It("omits alias when Machine has no IPs and requeues", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-empty")
		Expect(k8sClient.Create(ctx, makeAttachment("noip", infraName, "noip", "example.com", "clusters-empty"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeNodePool("noip-np", "clusters", "noip"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeMachine("machine-noip", "clusters-empty", "clusters/noip-np", nil))).To(Succeed())

		res := reconcileInfra()
		Expect(res.RequeueAfter).To(Equal(aliasPendingRequeue))
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
		Expect(k8sClient.Create(ctx, makeNodePool("dup-a-np", "clusters", "dup-a"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeNodePool("dup-b-np", "clusters", "dup-b"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeMachine("machine-dup-a", "clusters-dup-a", "clusters/dup-a-np", []string{"192.168.100.99"}))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeMachine("machine-dup-b", "clusters-dup-b", "clusters/dup-b-np", []string{"192.168.100.99"}))).To(Succeed())

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
		Expect(k8sClient.Create(ctx, makeNodePool("overlap-a-np", "clusters", "overlap-a"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeNodePool("overlap-b-np", "clusters", "overlap-b"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeMachine("machine-overlap-a", "clusters-overlap-a", "clusters/overlap-a-np", []string{"192.168.100.70", "192.168.100.71"}))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeMachine("machine-overlap-b", "clusters-overlap-b", "clusters/overlap-b-np", []string{"192.168.100.70"}))).To(Succeed())

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
		Expect(byName["overlap-a-kubernetes-hostname"].SourcePrefixRanges).To(Equal([]string{"192.168.100.71/32"}))
		Expect(byName).NotTo(HaveKey("overlap-b-kubernetes-hostname"))
		conflicts := 0
		for _, msg := range aggConflicts(infra) {
			if strings.Contains(msg, "192.168.100.70") {
				conflicts++
			}
		}
		Expect(conflicts).To(Equal(1))
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
		Expect(k8sClient.Create(ctx, makeNodePool("tri-a-np", "clusters", "tri-a"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeNodePool("tri-b-np", "clusters", "tri-b"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeNodePool("tri-c-np", "clusters", "tri-c"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeMachine("machine-tri-a", "clusters-triple-a", "clusters/tri-a-np", []string{"192.168.100.80"}))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeMachine("machine-tri-b", "clusters-triple-b", "clusters/tri-b-np", []string{"192.168.100.80"}))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeMachine("machine-tri-c", "clusters-triple-c", "clusters/tri-c-np", []string{"192.168.100.80"}))).To(Succeed())

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
		Expect(k8sClient.Create(ctx, makeNodePool("cap-np", "clusters", "cap"))).To(Succeed())

		generated := make([]string, 0, maxAliasSourcePrefixRanges+5)
		total := maxAliasSourcePrefixRanges + 5
		for i := 0; i < total; i++ {
			ip := fmt.Sprintf("10.200.%d.%d", 1+i/250, 1+i%250)
			generated = append(generated, ip+"/32")
			Expect(k8sClient.Create(ctx, makeMachine(fmt.Sprintf("machine-cap-%03d", i), "clusters-cap", "clusters/cap-np", []string{ip}))).To(Succeed())
		}
		sort.Strings(generated)
		expected := generated[:maxAliasSourcePrefixRanges]

		var att hostedclusterv1alpha1.InfraClusterAttachment
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cap", Namespace: "default"}, &att)).To(Succeed())
		dbg, pending := reconciler.resolveAttachmentSourceCIDRs(ctx, &att, "10.200.0.0/16")
		Expect(pending).To(BeFalse())
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

	It("removes alias when Machine is deleted", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-alpha")
		Expect(k8sClient.Create(ctx, makeAttachment("alpha", infraName, "alpha", "example.com", "clusters-alpha"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeNodePool("alpha-np-del", "clusters", "alpha"))).To(Succeed())
		m := makeMachine("machine-alpha-1", "clusters-alpha", "clusters/alpha-np-del", []string{"192.168.100.10"})
		Expect(k8sClient.Create(ctx, m)).To(Succeed())

		reconcileInfra()
		Expect(getProxy().Spec.Backends).To(ContainElement(HaveField("Name", "alpha-kubernetes-hostname")))

		Expect(k8sClient.Delete(ctx, m)).To(Succeed())
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
		Expect(k8sClient.Create(ctx, makeNodePool("derived-np", "clusters", "derived"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeMachine("machine-derived", "clusters-derived", "clusters/derived-np", []string{"192.168.100.77"}))).To(Succeed())

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
		Expect(k8sClient.Create(ctx, makeNodePool("alpha-np-dns", "clusters", "alpha"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeNodePool("bravo-np-dns", "clusters", "bravo"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeMachine("machine-alpha-1", "clusters-alpha", "clusters/alpha-np-dns", []string{"192.168.100.10"}))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeMachine("machine-bravo-1", "clusters-bravo", "clusters/bravo-np-dns", []string{"192.168.100.11"}))).To(Succeed())

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
		Expect(k8sClient.Create(ctx, makeNodePool("active-np", "clusters", "active"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeMachine("machine-active", "clusters-alpha", "clusters/active-np", []string{"192.168.100.10"}))).To(Succeed())
		terminating := makeAttachment("terminating", infraName, "terminating", "example.com", "clusters-alpha")
		terminating.Finalizers = []string{"test.finalizer"}
		Expect(k8sClient.Create(ctx, terminating)).To(Succeed())
		Expect(k8sClient.Delete(ctx, terminating)).To(Succeed())

		reconcileInfra()
		proxy := getProxy()
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
		longName := "shared-infra-production-cluster"
		Expect(longName).To(HaveLen(31))
		Expect(k8sClient.Create(ctx, makeAttachment(longName, infraName, "longname", "example.com", "clusters-longname"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeNodePool("longname-np", "clusters", longName))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeMachine("machine-longname", "clusters-longname", "clusters/longname-np", []string{"192.168.100.90"}))).To(Succeed())

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
		Expect(k8sClient.Create(ctx, makeNodePool("alpha-np-fqdn", "clusters", "alpha"))).To(Succeed())
		reconcileInfra()
		proxy := getProxy()
		Expect(proxy.Spec.Backends).To(ContainElement(HaveField("Hostname", "api.alpha.example.com")))
		Expect(proxy.Spec.Backends).To(ContainElement(HaveField("Hostname", "oauth.alpha.example.com")))
	})

	It("requeues pending when KubeVirt NodePool has no addresses", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-pending")
		Expect(k8sClient.Create(ctx, makeAttachment("pending", infraName, "pending", "example.com", "clusters-pending"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeNodePool("pending-np", "clusters", "pending"))).To(Succeed())
		res := reconcileInfra()
		Expect(res.RequeueAfter).To(Equal(aliasPendingRequeue))
	})

	It("safety requeues every 5m when aliases are present", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-safety")
		Expect(k8sClient.Create(ctx, makeAttachment("safety", infraName, "safety", "example.com", "clusters-safety"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeNodePool("safety-np", "clusters", "safety"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeMachine("machine-safety", "clusters-safety", "clusters/safety-np", []string{"192.168.100.99"}))).To(Succeed())
		res := reconcileInfra()
		Expect(res.RequeueAfter).To(Equal(aliasSafetyRequeue))
	})

	It("ignores non-KubeVirt NodePools", func() {
		createAliasInfra()
		ensureNamespace(ctx, "clusters-ignore")
		Expect(k8sClient.Create(ctx, makeAttachment("ignore", infraName, "ignore", "example.com", "clusters-ignore"))).To(Succeed())
		np := makeNodePool("ignore-np", "clusters", "ignore")
		_ = unstructured.SetNestedField(np.Object, "AWS", "spec", "platform", "type")
		Expect(k8sClient.Create(ctx, np)).To(Succeed())
		Expect(k8sClient.Create(ctx, makeMachine("machine-ignore", "clusters-ignore", "clusters/ignore-np", []string{"192.168.100.99"}))).To(Succeed())
		res := reconcileInfra()
		// No KubeVirt pool => no alias, no pending requeue
		Expect(res.RequeueAfter).To(BeZero())
		proxy := getProxy()
		for _, b := range proxy.Spec.Backends {
			Expect(b.Name).NotTo(ContainSubstring("kubernetes-hostname"))
		}
	})

	It("filters Machine addresses outside Infra CIDR and handles spaces", func() {
		ensureNamespace(ctx, "clusters")
		ensureNamespace(ctx, "clusters-space2")
		// Use cap infra? create dedicated
		cidr := "192.168.100.0/24"
		att := makeAttachment("space2", "alias-infra", "space2", "example.com", "clusters-space2")
		// Infra not needed for direct resolve test; create it for reconcile? Use alias-infra
		createAliasInfra()
		Expect(k8sClient.Create(ctx, att)).To(Succeed())
		Expect(k8sClient.Create(ctx, makeNodePool("space2-np", "clusters", "space2"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeMachine("machine-space2", "clusters-space2", "clusters/space2-np", []string{" 192.168.100.60 "}))).To(Succeed())
		r := &InfraReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		cidrs, pending := r.resolveAttachmentSourceCIDRs(context.Background(), att, cidr)
		Expect(pending).To(BeFalse())
		Expect(cidrs).To(Equal([]string{"192.168.100.60/32"}))
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
	It("generates stable per-attachment external Service names", func() {
		Expect(externalProxyServiceName("alpha")).To(Equal("alpha-proxy-external"))
		longName := externalProxyServiceName(strings.Repeat("attachment", 20))
		Expect(longName).To(HaveLen(63))
		Expect(externalProxyServiceName(strings.Repeat("attachment", 20))).To(Equal(longName))
	})

	It("normalizes omitted HostedCluster namespaces for routing defaults", func() {
		att := makeAttachment("defaulted", "shared-vlan", "defaulted", "example.com", "")
		att.Spec.HostedClusterRef.Namespace = ""

		view := attachmentFromAttachment(att)

		Expect(view.hostedClusterRef.Namespace).To(Equal("clusters"))
		Expect(view.controlPlaneNamespace).To(Equal("clusters-defaulted"))
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
			"shared-infra-production-cluster",
			"clusters-production-region-one-us-east-worker",
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

		a := backendNamePrefix(attachmentView{name: strings.Repeat("a", 45) + "-one"})
		b := backendNamePrefix(attachmentView{name: strings.Repeat("a", 45) + "-two"})
		Expect(a).To(HaveLen(40))
		Expect(b).To(HaveLen(40))
	})
})

var _ = Describe("resolveAttachmentSourceCIDRs edge cases", func() {
	It("returns nil for empty CIDR and invalid CIDR", func() {
		r := &InfraReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		att := makeAttachment("x", "y", "x", "example.com", "clusters-x")
		c, p := r.resolveAttachmentSourceCIDRs(context.Background(), att, "")
		Expect(c).To(BeNil())
		Expect(p).To(BeFalse())
		c, p = r.resolveAttachmentSourceCIDRs(context.Background(), att, "not-a-cidr")
		Expect(c).To(BeNil())
		Expect(p).To(BeFalse())
	})
	It("filters correctly when Machine IPs contain spaces and outside CIDR", func() {
		ensureNamespace(context.Background(), "clusters")
		ensureNamespace(context.Background(), "clusters-space")
		att := makeAttachment("space", "alias-infra", "space", "example.com", "clusters-space")
		Expect(k8sClient.Create(context.Background(), att)).To(Succeed())
		np := makeNodePool("space-np", "clusters", "space")
		Expect(k8sClient.Create(context.Background(), np)).To(Succeed())
		m := makeMachine("machine-space", "clusters-space", "clusters/space-np", []string{" 192.168.100.60 "})
		Expect(k8sClient.Create(context.Background(), m)).To(Succeed())
		r := &InfraReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		cidrs, pending := r.resolveAttachmentSourceCIDRs(context.Background(), att, "192.168.100.0/24")
		Expect(pending).To(BeFalse())
		Expect(cidrs).To(Equal([]string{"192.168.100.60/32"}))
		Expect(k8sClient.Delete(context.Background(), m)).To(Succeed())
		Expect(k8sClient.Delete(context.Background(), np)).To(Succeed())
		Expect(k8sClient.Delete(context.Background(), att)).To(Succeed())
	})
	It("returns pending when KubeVirt NodePool has no valid addresses", func() {
		ensureNamespace(context.Background(), "clusters")
		ensureNamespace(context.Background(), "clusters-pending2")
		att := makeAttachment("pending2", "alias-infra", "pending2", "example.com", "clusters-pending2")
		Expect(k8sClient.Create(context.Background(), att)).To(Succeed())
		np := makeNodePool("pending2-np", "clusters", "pending2")
		Expect(k8sClient.Create(context.Background(), np)).To(Succeed())
		r := &InfraReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		cidrs, pending := r.resolveAttachmentSourceCIDRs(context.Background(), att, "192.168.100.0/24")
		Expect(cidrs).To(BeNil())
		Expect(pending).To(BeTrue())
		Expect(k8sClient.Delete(context.Background(), np)).To(Succeed())
		Expect(k8sClient.Delete(context.Background(), att)).To(Succeed())
	})
})
