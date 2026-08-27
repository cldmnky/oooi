package proxy

import (
	"testing"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	tcp_proxy "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	hostedclusterv1alpha1 "github.com/cldmnky/oooi/api/v1alpha1"
)

func TestXDSServer_buildEnvoyResources_SourceIPAliases_SingleCluster(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, hostedclusterv1alpha1.AddToScheme(scheme))
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	xs := &XDSServer{client: k8sClient, proxies: make(map[string]*hostedclusterv1alpha1.ProxyServer)}

	proxy := &hostedclusterv1alpha1.ProxyServer{
		ObjectMeta: metav1.ObjectMeta{Name: "test-proxy", Namespace: "default"},
		Spec: hostedclusterv1alpha1.ProxyServerSpec{
			Backends: []hostedclusterv1alpha1.ProxyBackend{
				{
					Name:               "alpha-kube-apiserver-kubernetes-hostname",
					Hostname:           "kubernetes",
					AlternateHostnames: []string{"kubernetes.default", "kubernetes.default.svc", "kubernetes.default.svc.cluster.local", "kubernetes.alpha.example.com"},
					SourcePrefixRanges: []string{"192.168.100.10/32"},
					Port:               443,
					TargetService:      "kube-apiserver",
					TargetPort:         6443,
					TargetNamespace:    "clusters-alpha",
					Protocol:           "TCP",
					TimeoutSeconds:     30,
				},
			},
		},
	}
	listeners, clusters, err := xs.buildEnvoyResources(proxy)
	require.NoError(t, err)
	require.Len(t, listeners, 1)
	require.Len(t, clusters, 1)
	l := listeners[0].(*listener.Listener)
	require.Len(t, l.FilterChains, 1)
	fc := l.FilterChains[0]
	require.NotNil(t, fc.FilterChainMatch)
	assert.Equal(t, []string{"kubernetes", "kubernetes.default", "kubernetes.default.svc", "kubernetes.default.svc.cluster.local", "kubernetes.alpha.example.com"}, fc.FilterChainMatch.ServerNames)
	assert.Equal(t, "tls", fc.FilterChainMatch.TransportProtocol)
	require.Len(t, fc.FilterChainMatch.SourcePrefixRanges, 1)
	assert.Equal(t, "192.168.100.10", fc.FilterChainMatch.SourcePrefixRanges[0].AddressPrefix)
	assert.Equal(t, uint32(32), fc.FilterChainMatch.SourcePrefixRanges[0].PrefixLen.Value)
	// Cluster should still be LOGICAL_DNS to kube-apiserver service
	c := clusters[0].(*cluster.Cluster)
	assert.Equal(t, "test-proxy-alpha-kube-apiserver-kubernetes-hostname", c.Name)
	// TLS inspector present for SNI port
	require.NotEmpty(t, l.ListenerFilters)
}

func TestXDSServer_buildEnvoyResources_SourceIPAliases_MultipleClusters(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, hostedclusterv1alpha1.AddToScheme(scheme))
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	xs := &XDSServer{client: k8sClient, proxies: make(map[string]*hostedclusterv1alpha1.ProxyServer)}

	proxy := &hostedclusterv1alpha1.ProxyServer{
		ObjectMeta: metav1.ObjectMeta{Name: "test-proxy", Namespace: "default"},
		Spec: hostedclusterv1alpha1.ProxyServerSpec{
			Backends: []hostedclusterv1alpha1.ProxyBackend{
				{
					Name:               "alpha-kube-apiserver-kubernetes-hostname",
					Hostname:           "kubernetes",
					AlternateHostnames: []string{"kubernetes.default", "kubernetes.default.svc", "kubernetes.default.svc.cluster.local", "kubernetes.alpha.example.com"},
					SourcePrefixRanges: []string{"192.168.100.10/32"},
					Port:               443,
					TargetService:      "kube-apiserver",
					TargetPort:         6443,
					TargetNamespace:    "clusters-alpha",
					Protocol:           "TCP",
					TimeoutSeconds:     30,
				},
				{
					Name:               "bravo-kube-apiserver-kubernetes-hostname",
					Hostname:           "kubernetes",
					AlternateHostnames: []string{"kubernetes.default", "kubernetes.default.svc", "kubernetes.default.svc.cluster.local", "kubernetes.bravo.example.com"},
					SourcePrefixRanges: []string{"192.168.100.11/32", "192.168.100.12/32"},
					Port:               443,
					TargetService:      "kube-apiserver",
					TargetPort:         6443,
					TargetNamespace:    "clusters-bravo",
					Protocol:           "TCP",
					TimeoutSeconds:     30,
				},
				{
					Name:            "alpha-kube-apiserver",
					Hostname:        "api.alpha.example.com",
					Port:            6443,
					TargetService:   "kube-apiserver",
					TargetPort:      6443,
					TargetNamespace: "clusters-alpha",
					Protocol:        "TCP",
					TimeoutSeconds:  30,
				},
			},
		},
	}
	listeners, clusters, err := xs.buildEnvoyResources(proxy)
	require.NoError(t, err)
	// Port 443 and 6443 listeners
	require.Len(t, listeners, 2)
	require.Len(t, clusters, 3)
	// Find 443 listener
	var l443 *listener.Listener
	for _, l := range listeners {
		ll := l.(*listener.Listener)
		if ll.Address.GetSocketAddress().GetPortValue() == 443 {
			l443 = ll
		}
	}
	require.NotNil(t, l443)
	// FQDN api not on 443, so 443 should have exactly 2 alias chains plus maybe fallback? No konnectivity fallback because no konnectivity backend
	assert.Len(t, l443.FilterChains, 2)
	for _, fc := range l443.FilterChains {
		assert.NotEmpty(t, fc.FilterChainMatch.SourcePrefixRanges)
		assert.NotEmpty(t, fc.FilterChainMatch.ServerNames)
	}
	// Verify disjoint source ranges
	srcMap := map[string]bool{}
	for _, fc := range l443.FilterChains {
		for _, r := range fc.FilterChainMatch.SourcePrefixRanges {
			key := r.AddressPrefix + "/32"
			assert.False(t, srcMap[key], "duplicate source %s", key)
			srcMap[key] = true
		}
	}
	assert.True(t, srcMap["192.168.100.10/32"])
	assert.True(t, srcMap["192.168.100.11/32"])
	assert.True(t, srcMap["192.168.100.12/32"])

	// 6443 listener with single backend should still be plain TCP? In this case 6443 has only one backend (alpha), so plain TCP
	var l6443 *listener.Listener
	for _, l := range listeners {
		ll := l.(*listener.Listener)
		if ll.Address.GetSocketAddress().GetPortValue() == 6443 {
			l6443 = ll
		}
	}
	require.NotNil(t, l6443)
	// Single backend on 6443 => plain TCP catch-all, no TLS inspector
	assert.Empty(t, l6443.ListenerFilters)
	assert.Len(t, l6443.FilterChains, 1)
	assert.Nil(t, l6443.FilterChains[0].FilterChainMatch)
}

func TestXDSServer_buildEnvoyResources_MultiBackend6443_SNI(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, hostedclusterv1alpha1.AddToScheme(scheme))
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	xs := &XDSServer{client: k8sClient, proxies: make(map[string]*hostedclusterv1alpha1.ProxyServer)}

	proxy := &hostedclusterv1alpha1.ProxyServer{
		ObjectMeta: metav1.ObjectMeta{Name: "test-proxy", Namespace: "default"},
		Spec: hostedclusterv1alpha1.ProxyServerSpec{
			Backends: []hostedclusterv1alpha1.ProxyBackend{
				{Name: "alpha-kube-apiserver", Hostname: "api.alpha.example.com", Port: 6443, TargetService: "kube-apiserver", TargetPort: 6443, TargetNamespace: "clusters-alpha", Protocol: "TCP", TimeoutSeconds: 30},
				{Name: "bravo-kube-apiserver", Hostname: "api.bravo.example.com", Port: 6443, TargetService: "kube-apiserver", TargetPort: 6443, TargetNamespace: "clusters-bravo", Protocol: "TCP", TimeoutSeconds: 30},
			},
		},
	}
	listeners, clusters, err := xs.buildEnvoyResources(proxy)
	require.NoError(t, err)
	require.Len(t, listeners, 1)
	require.Len(t, clusters, 2)
	l := listeners[0].(*listener.Listener)
	assert.Equal(t, uint32(6443), l.Address.GetSocketAddress().GetPortValue())
	// For multi-backend 6443, should be SNI-routed with TLS inspector, not plain TCP
	require.NotEmpty(t, l.ListenerFilters, "multi-backend 6443 should have TLS inspector")
	assert.Len(t, l.FilterChains, 2)
	for _, fc := range l.FilterChains {
		require.NotNil(t, fc.FilterChainMatch)
		assert.NotEmpty(t, fc.FilterChainMatch.ServerNames)
		assert.Equal(t, "tls", fc.FilterChainMatch.TransportProtocol)
	}
	hostnames := map[string]bool{}
	for _, fc := range l.FilterChains {
		hostnames[fc.FilterChainMatch.ServerNames[0]] = true
	}
	assert.True(t, hostnames["api.alpha.example.com"])
	assert.True(t, hostnames["api.bravo.example.com"])
}

func TestXDSServer_buildEnvoyResources_SingleSourceScoped6443_NoSNI(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, hostedclusterv1alpha1.AddToScheme(scheme))
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	xs := &XDSServer{client: k8sClient, proxies: make(map[string]*hostedclusterv1alpha1.ProxyServer)}

	proxy := &hostedclusterv1alpha1.ProxyServer{
		ObjectMeta: metav1.ObjectMeta{Name: "test-proxy", Namespace: "default"},
		Spec: hostedclusterv1alpha1.ProxyServerSpec{
			Backends: []hostedclusterv1alpha1.ProxyBackend{
				{
					Name:               "alpha-kubernetes-service",
					Hostname:           "kubernetes",
					SourcePrefixRanges: []string{"192.168.100.10/32"},
					Port:               6443,
					TargetService:      "kube-apiserver",
					TargetPort:         6443,
					TargetNamespace:    "clusters-alpha",
					Protocol:           "TCP",
					TimeoutSeconds:     30,
				},
			},
		},
	}

	listeners, clusters, err := xs.buildEnvoyResources(proxy)
	require.NoError(t, err)
	require.Len(t, listeners, 1)
	require.Len(t, clusters, 1)

	l := listeners[0].(*listener.Listener)
	require.NotEmpty(t, l.ListenerFilters, "source-scoped 6443 must use TLS inspection")
	require.Len(t, l.FilterChains, 1)
	require.NotNil(t, l.FilterChains[0].FilterChainMatch)
	assert.Empty(t, l.FilterChains[0].FilterChainMatch.ServerNames)
	assert.Equal(t, "tls", l.FilterChains[0].FilterChainMatch.TransportProtocol)
	require.Len(t, l.FilterChains[0].FilterChainMatch.SourcePrefixRanges, 1)
	assert.Equal(t, "192.168.100.10", l.FilterChains[0].FilterChainMatch.SourcePrefixRanges[0].AddressPrefix)
}

func TestXDSServer_buildEnvoyResources_KubernetesServiceAliasUsesSourceOnlyMatch(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, hostedclusterv1alpha1.AddToScheme(scheme))
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	xs := &XDSServer{client: k8sClient, proxies: make(map[string]*hostedclusterv1alpha1.ProxyServer)}

	proxy := &hostedclusterv1alpha1.ProxyServer{
		ObjectMeta: metav1.ObjectMeta{Name: "test-proxy", Namespace: "default"},
		Spec: hostedclusterv1alpha1.ProxyServerSpec{
			Backends: []hostedclusterv1alpha1.ProxyBackend{
				{Name: "alpha-kube-apiserver", Hostname: "api.alpha.example.com", Port: 6443, TargetService: "kube-apiserver", TargetPort: 6443, TargetNamespace: "clusters-alpha", Protocol: "TCP", TimeoutSeconds: 30},
				{Name: "alpha-kubernetes-service", Hostname: "kubernetes", AlternateHostnames: []string{"kubernetes.default.svc"}, SourcePrefixRanges: []string{"192.168.100.10/32"}, Port: 6443, TargetService: "kube-apiserver", TargetPort: 6443, TargetNamespace: "clusters-alpha", Protocol: "TCP", TimeoutSeconds: 30},
				{Name: "bravo-kube-apiserver", Hostname: "api.bravo.example.com", Port: 6443, TargetService: "kube-apiserver", TargetPort: 6443, TargetNamespace: "clusters-bravo", Protocol: "TCP", TimeoutSeconds: 30},
				{Name: "bravo-kubernetes-service", Hostname: "kubernetes", AlternateHostnames: []string{"kubernetes.default.svc"}, SourcePrefixRanges: []string{"192.168.100.11/32"}, Port: 6443, TargetService: "kube-apiserver", TargetPort: 6443, TargetNamespace: "clusters-bravo", Protocol: "TCP", TimeoutSeconds: 30},
			},
		},
	}

	listeners, clusters, err := xs.buildEnvoyResources(proxy)
	require.NoError(t, err)
	require.Len(t, listeners, 1)
	require.Len(t, clusters, 4)

	l := listeners[0].(*listener.Listener)
	assert.NotEmpty(t, l.ListenerFilters)
	require.Len(t, l.FilterChains, 4)
	var sourceOnly int
	for _, fc := range l.FilterChains {
		if len(fc.FilterChainMatch.SourcePrefixRanges) == 0 {
			continue
		}
		sourceOnly++
		assert.Empty(t, fc.FilterChainMatch.ServerNames)
		assert.Equal(t, "tls", fc.FilterChainMatch.TransportProtocol)
	}
	assert.Equal(t, 2, sourceOnly)
}

func TestXDSServer_buildEnvoyResources_SourceIP_MultipleCIDRs(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, hostedclusterv1alpha1.AddToScheme(scheme))
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	xs := &XDSServer{client: k8sClient, proxies: make(map[string]*hostedclusterv1alpha1.ProxyServer)}

	proxy := &hostedclusterv1alpha1.ProxyServer{
		ObjectMeta: metav1.ObjectMeta{Name: "test-proxy", Namespace: "default"},
		Spec: hostedclusterv1alpha1.ProxyServerSpec{
			Backends: []hostedclusterv1alpha1.ProxyBackend{
				{
					Name:               "alpha-alias",
					Hostname:           "kubernetes",
					AlternateHostnames: []string{"kubernetes.default"},
					SourcePrefixRanges: []string{"192.168.100.10/32", "192.168.100.20/32", "10.0.0.0/24"},
					Port:               443,
					TargetService:      "kube-apiserver",
					TargetPort:         6443,
					TargetNamespace:    "clusters-alpha",
					Protocol:           "TCP",
					TimeoutSeconds:     30,
				},
			},
		},
	}
	listeners, _, err := xs.buildEnvoyResources(proxy)
	require.NoError(t, err)
	l := listeners[0].(*listener.Listener)
	fc := l.FilterChains[0]
	require.Len(t, fc.FilterChainMatch.SourcePrefixRanges, 3)
	// Verify prefix lengths
	m := map[string]uint32{}
	for _, r := range fc.FilterChainMatch.SourcePrefixRanges {
		m[r.AddressPrefix] = r.PrefixLen.Value
	}
	assert.Equal(t, uint32(32), m["192.168.100.10"])
	assert.Equal(t, uint32(32), m["192.168.100.20"])
	assert.Equal(t, uint32(24), m["10.0.0.0"])
}

func TestXDSServer_buildEnvoyResources_SourceIP_InvalidCIDR_Skipped(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, hostedclusterv1alpha1.AddToScheme(scheme))
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	xs := &XDSServer{client: k8sClient, proxies: make(map[string]*hostedclusterv1alpha1.ProxyServer)}

	proxy := &hostedclusterv1alpha1.ProxyServer{
		ObjectMeta: metav1.ObjectMeta{Name: "test-proxy", Namespace: "default"},
		Spec: hostedclusterv1alpha1.ProxyServerSpec{
			Backends: []hostedclusterv1alpha1.ProxyBackend{
				{
					Name:               "alpha-alias",
					Hostname:           "kubernetes",
					SourcePrefixRanges: []string{"not-a-cidr", "192.168.100.10/32"},
					Port:               443,
					TargetService:      "kube-apiserver",
					TargetPort:         6443,
					TargetNamespace:    "clusters-alpha",
					Protocol:           "TCP",
					TimeoutSeconds:     30,
				},
			},
		},
	}
	listeners, _, err := xs.buildEnvoyResources(proxy)
	require.NoError(t, err)
	l := listeners[0].(*listener.Listener)
	fc := l.FilterChains[0]
	// Only valid CIDR should be kept
	require.Len(t, fc.FilterChainMatch.SourcePrefixRanges, 1)
	assert.Equal(t, "192.168.100.10", fc.FilterChainMatch.SourcePrefixRanges[0].AddressPrefix)
}

func TestXDSServer_buildEnvoyResources_SourceIP_AllInvalidCIDRsFailClosed(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, hostedclusterv1alpha1.AddToScheme(scheme))
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	xs := &XDSServer{client: k8sClient, proxies: make(map[string]*hostedclusterv1alpha1.ProxyServer)}

	proxy := &hostedclusterv1alpha1.ProxyServer{
		ObjectMeta: metav1.ObjectMeta{Name: "test-proxy", Namespace: "default"},
		Spec: hostedclusterv1alpha1.ProxyServerSpec{
			Backends: []hostedclusterv1alpha1.ProxyBackend{
				{
					Name:               "alpha-alias",
					Hostname:           "kubernetes",
					SourcePrefixRanges: []string{"not-a-cidr"},
					Port:               443,
					TargetService:      "kube-apiserver",
					TargetPort:         6443,
					TargetNamespace:    "clusters-alpha",
					Protocol:           "TCP",
					TimeoutSeconds:     30,
				},
			},
		},
	}

	_, _, err := xs.buildEnvoyResources(proxy)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `backend "alpha-alias" has no valid source prefix ranges`)
}

func TestXDSServer_buildEnvoyResources_SourceIPAliases_WithKonnectivityFallback(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, hostedclusterv1alpha1.AddToScheme(scheme))
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	xs := &XDSServer{client: k8sClient, proxies: make(map[string]*hostedclusterv1alpha1.ProxyServer)}

	proxy := &hostedclusterv1alpha1.ProxyServer{
		ObjectMeta: metav1.ObjectMeta{Name: "test-proxy", Namespace: "default"},
		Spec: hostedclusterv1alpha1.ProxyServerSpec{
			Backends: []hostedclusterv1alpha1.ProxyBackend{
				{
					Name:               "alpha-kube-apiserver-kubernetes-hostname",
					Hostname:           "kubernetes",
					AlternateHostnames: []string{"kubernetes.default"},
					SourcePrefixRanges: []string{"192.168.100.10/32"},
					Port:               443,
					TargetService:      "kube-apiserver",
					TargetPort:         6443,
					TargetNamespace:    "clusters-alpha",
					Protocol:           "TCP",
					TimeoutSeconds:     30,
				},
				{
					Name:            "konnectivity-server",
					Hostname:        "konnectivity.alpha.example.com",
					Port:            443,
					TargetService:   "konnectivity-server",
					TargetPort:      8091,
					TargetNamespace: "clusters-alpha",
					Protocol:        "TCP",
					TimeoutSeconds:  30,
				},
			},
		},
	}
	listeners, clusters, err := xs.buildEnvoyResources(proxy)
	require.NoError(t, err)
	require.Len(t, clusters, 2)
	l := listeners[0].(*listener.Listener)
	// 2 SNI chains + 1 fallback = 3
	require.Len(t, l.FilterChains, 3)
	// Fallback should be last
	fallback := l.FilterChains[2]
	assert.Nil(t, fallback.FilterChainMatch)
	// Verify fallback cluster is konnectivity
	typed := fallback.Filters[0].GetTypedConfig()
	var tcp tcp_proxy.TcpProxy
	err = anypb.UnmarshalTo(typed, &tcp, proto.UnmarshalOptions{})
	require.NoError(t, err)
	assert.Equal(t, "test-proxy-konnectivity-server", tcp.GetCluster())

	// Alias chain should have source ranges
	var aliasFC *listener.FilterChain
	for _, fc := range l.FilterChains {
		if fc.FilterChainMatch != nil && len(fc.FilterChainMatch.SourcePrefixRanges) > 0 {
			aliasFC = fc
			break
		}
	}
	require.NotNil(t, aliasFC)
	assert.Contains(t, aliasFC.FilterChainMatch.ServerNames, "kubernetes")
}

func TestXDSServer_buildEnvoyResources_SourceIPAliases_NoFallbackForSourceScopedKonnectivity(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, hostedclusterv1alpha1.AddToScheme(scheme))
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	xs := &XDSServer{client: k8sClient, proxies: make(map[string]*hostedclusterv1alpha1.ProxyServer)}

	proxy := &hostedclusterv1alpha1.ProxyServer{
		ObjectMeta: metav1.ObjectMeta{Name: "test-proxy", Namespace: "default"},
		Spec: hostedclusterv1alpha1.ProxyServerSpec{
			Backends: []hostedclusterv1alpha1.ProxyBackend{
				{
					Name:               "konnectivity-server",
					Hostname:           "konnectivity.alpha.example.com",
					SourcePrefixRanges: []string{"192.168.100.10/32"},
					Port:               443,
					TargetService:      "konnectivity-server",
					TargetPort:         8091,
					TargetNamespace:    "clusters-alpha",
					Protocol:           "TCP",
					TimeoutSeconds:     30,
				},
			},
		},
	}
	listeners, _, err := xs.buildEnvoyResources(proxy)
	require.NoError(t, err)
	l := listeners[0].(*listener.Listener)
	// Source-scoped konnectivity should NOT create fallback
	assert.Len(t, l.FilterChains, 1)
	assert.NotNil(t, l.FilterChains[0].FilterChainMatch)
	assert.Len(t, l.FilterChains[0].FilterChainMatch.SourcePrefixRanges, 1)
}

func TestXDSServer_buildEnvoyResources_FallbackChainForIP_Konnectivity_SourceIPBeatsFallback(t *testing.T) {
	// Alias chains with SNI+source must be more specific than fallback (no SNI).
	// This test ensures alias is not swallowed by fallback matching.
	scheme := runtime.NewScheme()
	require.NoError(t, hostedclusterv1alpha1.AddToScheme(scheme))
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	xs := &XDSServer{client: k8sClient, proxies: make(map[string]*hostedclusterv1alpha1.ProxyServer)}

	proxy := &hostedclusterv1alpha1.ProxyServer{
		ObjectMeta: metav1.ObjectMeta{Name: "test-proxy", Namespace: "default"},
		Spec: hostedclusterv1alpha1.ProxyServerSpec{
			Backends: []hostedclusterv1alpha1.ProxyBackend{
				{
					Name:               "alpha-alias",
					Hostname:           "kubernetes",
					AlternateHostnames: []string{"kubernetes.default"},
					SourcePrefixRanges: []string{"192.168.100.10/32"},
					Port:               443,
					TargetService:      "kube-apiserver",
					TargetPort:         6443,
					TargetNamespace:    "clusters-alpha",
					Protocol:           "TCP",
					TimeoutSeconds:     30,
				},
				{
					Name:            "konnectivity-server",
					Hostname:        "konnectivity.alpha.example.com",
					Port:            443,
					TargetService:   "konnectivity-server",
					TargetPort:      8091,
					TargetNamespace: "clusters-alpha",
					Protocol:        "TCP",
					TimeoutSeconds:  30,
				},
			},
		},
	}
	listeners, _, err := xs.buildEnvoyResources(proxy)
	require.NoError(t, err)
	l := listeners[0].(*listener.Listener)
	// FQDN konnectivity chain (SNI without source) + alias (SNI+source) + fallback (catch-all) = 3
	require.Len(t, l.FilterChains, 3)
	// Find alias and verify it has source and SNI, thus more specific than fallback
	var aliasFound, fallbackFound bool
	for _, fc := range l.FilterChains {
		if fc.FilterChainMatch != nil && len(fc.FilterChainMatch.SourcePrefixRanges) > 0 && len(fc.FilterChainMatch.ServerNames) > 0 {
			aliasFound = true
			assert.Equal(t, "tls", fc.FilterChainMatch.TransportProtocol)
		}
		if fc.FilterChainMatch == nil {
			fallbackFound = true
		}
	}
	assert.True(t, aliasFound)
	assert.True(t, fallbackFound)
}

func TestXDSServer_buildEnvoyResources_ClusterConfiguration_SourceIP(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, hostedclusterv1alpha1.AddToScheme(scheme))
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	xs := &XDSServer{client: k8sClient, proxies: make(map[string]*hostedclusterv1alpha1.ProxyServer)}

	// Direct IP/hostname handling should still work with source IP aliases
	proxy := &hostedclusterv1alpha1.ProxyServer{
		ObjectMeta: metav1.ObjectMeta{Name: "test-proxy", Namespace: "default"},
		Spec: hostedclusterv1alpha1.ProxyServerSpec{
			Backends: []hostedclusterv1alpha1.ProxyBackend{
				{
					Name:               "apps-http",
					Hostname:           "*.apps.alpha.example.com",
					SourcePrefixRanges: []string{"192.168.100.10/32"},
					Port:               80,
					TargetService:      "192.168.100.99",
					TargetPort:         80,
					TargetNamespace:    "default",
					Protocol:           "TCP",
					TimeoutSeconds:     30,
				},
			},
		},
	}
	// Port 80 is plain TCP, source ranges should be ignored? Actually for plain TCP we currently ignore SourcePrefixRanges because we create catch-all chain.
	// Let's verify behavior: for port 80, usePlainTCP true, so we create plainTCPCluster catch-all, no SNI/source handling.
	// This test documents current behavior: apps wildcard on 80 uses plain TCP, source IP not supported there.
	listeners, clusters, err := xs.buildEnvoyResources(proxy)
	require.NoError(t, err)
	require.Len(t, listeners, 1)
	require.Len(t, clusters, 1)
	l := listeners[0].(*listener.Listener)
	// Plain TCP => no source ranges
	assert.Len(t, l.FilterChains, 1)
	assert.Nil(t, l.FilterChains[0].FilterChainMatch)
	// Cluster should be STATIC for IP target
	c := clusters[0].(*cluster.Cluster)
	assert.Equal(t, cluster.Cluster_STATIC, c.GetType())
	assert.Equal(t, "192.168.100.10/32", proxy.Spec.Backends[0].SourcePrefixRanges[0])
	// Verify core type doesn't panic
	_ = core.CidrRange{}
}

func TestXDSServer_buildEnvoyResources_SourceIPAliases_AlternateHostnames(t *testing.T) {
	// Ensure alternate hostnames still counted correctly with source IP
	scheme := runtime.NewScheme()
	require.NoError(t, hostedclusterv1alpha1.AddToScheme(scheme))
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	xs := &XDSServer{client: k8sClient, proxies: make(map[string]*hostedclusterv1alpha1.ProxyServer)}

	proxy := &hostedclusterv1alpha1.ProxyServer{
		ObjectMeta: metav1.ObjectMeta{Name: "test-proxy", Namespace: "default"},
		Spec: hostedclusterv1alpha1.ProxyServerSpec{
			Backends: []hostedclusterv1alpha1.ProxyBackend{
				{
					Name:               "alias",
					Hostname:           "kubernetes",
					AlternateHostnames: []string{"kubernetes.default", "kubernetes.default.svc", "kubernetes.default.svc.cluster.local"},
					SourcePrefixRanges: []string{"192.168.100.10/32"},
					Port:               443,
					TargetService:      "kube-apiserver",
					TargetPort:         6443,
					TargetNamespace:    "default",
					Protocol:           "TCP",
					TimeoutSeconds:     30,
				},
			},
		},
	}
	listeners, _, err := xs.buildEnvoyResources(proxy)
	require.NoError(t, err)
	l := listeners[0].(*listener.Listener)
	require.Len(t, l.FilterChains, 1)
	fc := l.FilterChains[0]
	assert.Len(t, fc.FilterChainMatch.ServerNames, 4)
	assert.Contains(t, fc.FilterChainMatch.ServerNames, "kubernetes")
	assert.Contains(t, fc.FilterChainMatch.ServerNames, "kubernetes.default.svc.cluster.local")
}
