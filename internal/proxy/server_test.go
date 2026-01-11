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

package proxy

import (
	"context"
	"testing"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	hostedclusterv1alpha1 "github.com/cldmnky/oooi/api/v1alpha1"
)

func TestNewXDSServer(t *testing.T) {
	tests := []struct {
		name        string
		xdsPort     int32
		wantErr     bool
		description string
	}{
		{
			name:        "valid port",
			xdsPort:     18000,
			wantErr:     false,
			description: "should create xDS server with valid port",
		},
		{
			name:        "high port number",
			xdsPort:     50000,
			wantErr:     false,
			description: "should create xDS server with high port number",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, hostedclusterv1alpha1.AddToScheme(scheme))

			k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			xs, err := NewXDSServer(k8sClient, tt.xdsPort)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, xs)
			assert.NotNil(t, xs.client)
			assert.NotNil(t, xs.cache)
			assert.NotNil(t, xs.grpcServer)
			assert.NotNil(t, xs.proxies)
			assert.Equal(t, 0, xs.snapVersion)

			// Cleanup
			xs.Stop()
			time.Sleep(100 * time.Millisecond) // Allow server to stop
		})
	}
}

func TestXDSServer_UpdateProxyConfig(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, hostedclusterv1alpha1.AddToScheme(scheme))

	tests := []struct {
		name        string
		proxy       *hostedclusterv1alpha1.ProxyServer
		wantErr     bool
		description string
	}{
		{
			name: "single backend",
			proxy: &hostedclusterv1alpha1.ProxyServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-proxy",
					Namespace: "default",
				},
				Spec: hostedclusterv1alpha1.ProxyServerSpec{
					Backends: []hostedclusterv1alpha1.ProxyBackend{
						{
							Name:            "kube-apiserver",
							Hostname:        "api.test.example.com",
							Port:            6443,
							TargetService:   "kube-apiserver",
							TargetPort:      6443,
							TargetNamespace: "default",
							Protocol:        "TCP",
							TimeoutSeconds:  30,
						},
					},
				},
			},
			wantErr:     false,
			description: "should update config with single backend",
		},
		{
			name: "multiple backends same port",
			proxy: &hostedclusterv1alpha1.ProxyServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-proxy",
					Namespace: "default",
				},
				Spec: hostedclusterv1alpha1.ProxyServerSpec{
					Backends: []hostedclusterv1alpha1.ProxyBackend{
						{
							Name:            "kube-apiserver",
							Hostname:        "api.test.example.com",
							Port:            6443,
							TargetService:   "kube-apiserver",
							TargetPort:      6443,
							TargetNamespace: "default",
							Protocol:        "TCP",
							TimeoutSeconds:  30,
						},
						{
							Name:            "oauth-server",
							Hostname:        "oauth.test.example.com",
							Port:            6443,
							TargetService:   "oauth-openshift",
							TargetPort:      6443,
							TargetNamespace: "default",
							Protocol:        "TCP",
							TimeoutSeconds:  30,
						},
					},
				},
			},
			wantErr:     false,
			description: "should update config with multiple backends on same port",
		},
		{
			name: "multiple backends different ports",
			proxy: &hostedclusterv1alpha1.ProxyServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-proxy",
					Namespace: "default",
				},
				Spec: hostedclusterv1alpha1.ProxyServerSpec{
					Backends: []hostedclusterv1alpha1.ProxyBackend{
						{
							Name:            "kube-apiserver",
							Hostname:        "api.test.example.com",
							Port:            6443,
							TargetService:   "kube-apiserver",
							TargetPort:      6443,
							TargetNamespace: "default",
							Protocol:        "TCP",
							TimeoutSeconds:  30,
						},
						{
							Name:            "ignition",
							Hostname:        "ignition.test.example.com",
							Port:            22623,
							TargetService:   "ignition",
							TargetPort:      22623,
							TargetNamespace: "default",
							Protocol:        "TCP",
							TimeoutSeconds:  30,
						},
					},
				},
			},
			wantErr:     false,
			description: "should update config with multiple backends on different ports",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			xs, err := NewXDSServer(k8sClient, 0) // Use dynamic port allocation
			require.NoError(t, err)
			defer xs.Stop()

			ctx := context.Background()
			err = xs.UpdateProxyConfig(ctx, tt.proxy)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Verify proxy is stored
			xs.mu.RLock()
			storedProxy, exists := xs.proxies[tt.proxy.Name]
			snapVersion := xs.snapVersion
			xs.mu.RUnlock()

			assert.True(t, exists, "proxy should be stored")
			assert.Equal(t, tt.proxy.Name, storedProxy.Name)
			assert.Greater(t, snapVersion, 0, "snapshot version should be incremented")
		})
	}
}

func TestXDSServer_buildEnvoyResources(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, hostedclusterv1alpha1.AddToScheme(scheme))

	tests := []struct {
		name              string
		proxy             *hostedclusterv1alpha1.ProxyServer
		wantListenerCount int
		wantClusterCount  int
		wantErr           bool
		description       string
	}{
		{
			name: "single backend creates one listener and one cluster",
			proxy: &hostedclusterv1alpha1.ProxyServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-proxy",
					Namespace: "default",
				},
				Spec: hostedclusterv1alpha1.ProxyServerSpec{
					Backends: []hostedclusterv1alpha1.ProxyBackend{
						{
							Name:            "kube-apiserver",
							Hostname:        "api.test.example.com",
							Port:            6443,
							TargetService:   "kube-apiserver",
							TargetPort:      6443,
							TargetNamespace: "default",
							Protocol:        "TCP",
							TimeoutSeconds:  30,
						},
					},
				},
			},
			wantListenerCount: 1,
			wantClusterCount:  1,
			wantErr:           false,
			description:       "should create one listener and one cluster for single backend",
		},
		{
			name: "multiple backends same port creates one listener multiple clusters",
			proxy: &hostedclusterv1alpha1.ProxyServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-proxy",
					Namespace: "default",
				},
				Spec: hostedclusterv1alpha1.ProxyServerSpec{
					Backends: []hostedclusterv1alpha1.ProxyBackend{
						{
							Name:            "kube-apiserver",
							Hostname:        "api.test.example.com",
							Port:            6443,
							TargetService:   "kube-apiserver",
							TargetPort:      6443,
							TargetNamespace: "default",
							Protocol:        "TCP",
							TimeoutSeconds:  30,
						},
						{
							Name:            "oauth-server",
							Hostname:        "oauth.test.example.com",
							Port:            6443,
							TargetService:   "oauth-openshift",
							TargetPort:      6443,
							TargetNamespace: "default",
							Protocol:        "TCP",
							TimeoutSeconds:  30,
						},
					},
				},
			},
			wantListenerCount: 1,
			wantClusterCount:  2,
			wantErr:           false,
			description:       "should create one listener and two clusters for backends on same port",
		},
		{
			name: "multiple backends different ports creates multiple listeners and clusters",
			proxy: &hostedclusterv1alpha1.ProxyServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-proxy",
					Namespace: "default",
				},
				Spec: hostedclusterv1alpha1.ProxyServerSpec{
					Backends: []hostedclusterv1alpha1.ProxyBackend{
						{
							Name:            "kube-apiserver",
							Hostname:        "api.test.example.com",
							Port:            6443,
							TargetService:   "kube-apiserver",
							TargetPort:      6443,
							TargetNamespace: "default",
							Protocol:        "TCP",
							TimeoutSeconds:  30,
						},
						{
							Name:            "ignition",
							Hostname:        "ignition.test.example.com",
							Port:            22623,
							TargetService:   "ignition",
							TargetPort:      22623,
							TargetNamespace: "default",
							Protocol:        "TCP",
							TimeoutSeconds:  30,
						},
					},
				},
			},
			wantListenerCount: 2,
			wantClusterCount:  2,
			wantErr:           false,
			description:       "should create two listeners and two clusters for backends on different ports",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

			xs := &XDSServer{
				client:  k8sClient,
				proxies: make(map[string]*hostedclusterv1alpha1.ProxyServer),
			}

			listeners, clusters, err := xs.buildEnvoyResources(tt.proxy)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, listeners, tt.wantListenerCount, "unexpected listener count")
			assert.Len(t, clusters, tt.wantClusterCount, "unexpected cluster count")

			// Verify listener structure
			for _, l := range listeners {
				listenerProto := l.(*listener.Listener)
				assert.NotNil(t, listenerProto.Address)
				assert.NotEmpty(t, listenerProto.FilterChains)
				assert.NotEmpty(t, listenerProto.ListenerFilters, "should have TLS inspector")
			}

			// Verify cluster structure
			for _, c := range clusters {
				clusterProto := c.(*cluster.Cluster)
				assert.NotEmpty(t, clusterProto.Name)
				assert.NotNil(t, clusterProto.ConnectTimeout)
				assert.NotNil(t, clusterProto.LoadAssignment)
			}
		})
	}
}

func TestXDSServer_buildEnvoyResources_SNIRouting(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, hostedclusterv1alpha1.AddToScheme(scheme))

	proxy := &hostedclusterv1alpha1.ProxyServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-proxy",
			Namespace: "default",
		},
		Spec: hostedclusterv1alpha1.ProxyServerSpec{
			Backends: []hostedclusterv1alpha1.ProxyBackend{
				{
					Name:            "kube-apiserver",
					Hostname:        "api.test.example.com",
					Port:            6443,
					TargetService:   "kube-apiserver",
					TargetPort:      6443,
					TargetNamespace: "default",
					Protocol:        "TCP",
					TimeoutSeconds:  30,
				},
				{
					Name:            "oauth-server",
					Hostname:        "oauth.test.example.com",
					Port:            6443,
					TargetService:   "oauth-openshift",
					TargetPort:      6443,
					TargetNamespace: "default",
					Protocol:        "TCP",
					TimeoutSeconds:  30,
				},
			},
		},
	}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	xs := &XDSServer{
		client:  k8sClient,
		proxies: make(map[string]*hostedclusterv1alpha1.ProxyServer),
	}

	listeners, clusters, err := xs.buildEnvoyResources(proxy)
	require.NoError(t, err)
	require.Len(t, listeners, 1, "should have one listener for both backends on same port")
	require.Len(t, clusters, 2, "should have two clusters")

	// Verify listener has filter chains for SNI routing
	listenerProto := listeners[0].(*listener.Listener)
	assert.Len(t, listenerProto.FilterChains, 2, "should have two filter chains for SNI routing")

	// Verify each filter chain has correct SNI match
	hostnames := make(map[string]bool)
	for _, fc := range listenerProto.FilterChains {
		assert.NotNil(t, fc.FilterChainMatch, "filter chain should have match")
		assert.NotEmpty(t, fc.FilterChainMatch.ServerNames, "should have SNI hostname")
		hostnames[fc.FilterChainMatch.ServerNames[0]] = true
	}

	assert.True(t, hostnames["api.test.example.com"], "should have api hostname")
	assert.True(t, hostnames["oauth.test.example.com"], "should have oauth hostname")
}

func TestXDSServer_buildEnvoyResources_ClusterConfiguration(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, hostedclusterv1alpha1.AddToScheme(scheme))

	proxy := &hostedclusterv1alpha1.ProxyServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-proxy",
			Namespace: "default",
		},
		Spec: hostedclusterv1alpha1.ProxyServerSpec{
			Backends: []hostedclusterv1alpha1.ProxyBackend{
				{
					Name:            "kube-apiserver",
					Hostname:        "api.test.example.com",
					Port:            6443,
					TargetService:   "kube-apiserver",
					TargetPort:      6443,
					TargetNamespace: "openshift-kube-apiserver",
					Protocol:        "TCP",
					TimeoutSeconds:  45,
				},
			},
		},
	}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	xs := &XDSServer{
		client:  k8sClient,
		proxies: make(map[string]*hostedclusterv1alpha1.ProxyServer),
	}

	_, clusters, err := xs.buildEnvoyResources(proxy)
	require.NoError(t, err)
	require.Len(t, clusters, 1)

	clusterProto := clusters[0].(*cluster.Cluster)

	// Verify cluster name
	assert.Equal(t, "test-proxy-kube-apiserver", clusterProto.Name)

	// Verify connect timeout
	assert.Equal(t, int64(45), clusterProto.ConnectTimeout.Seconds)

	// Verify cluster type is LOGICAL_DNS
	assert.Equal(t, cluster.Cluster_LOGICAL_DNS, clusterProto.GetType())

	// Verify load balancing policy
	assert.Equal(t, cluster.Cluster_ROUND_ROBIN, clusterProto.LbPolicy)

	// Verify endpoint configuration
	require.NotNil(t, clusterProto.LoadAssignment)
	require.Len(t, clusterProto.LoadAssignment.Endpoints, 1)
	require.Len(t, clusterProto.LoadAssignment.Endpoints[0].LbEndpoints, 1)

	endpoint := clusterProto.LoadAssignment.Endpoints[0].LbEndpoints[0].GetEndpoint()
	require.NotNil(t, endpoint)

	socketAddr := endpoint.Address.GetSocketAddress()
	require.NotNil(t, socketAddr)
	assert.Equal(t, "kube-apiserver.openshift-kube-apiserver.svc.cluster.local", socketAddr.Address)
	assert.Equal(t, uint32(6443), socketAddr.GetPortValue())

	// Verify DNS lookup family
	assert.Equal(t, cluster.Cluster_V4_ONLY, clusterProto.DnsLookupFamily)
}

func TestXDSServer_RemoveProxyConfig(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, hostedclusterv1alpha1.AddToScheme(scheme))

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	xs, err := NewXDSServer(k8sClient, 18001)
	require.NoError(t, err)
	defer xs.Stop()

	// Add a proxy first
	proxy := &hostedclusterv1alpha1.ProxyServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-proxy",
			Namespace: "default",
		},
		Spec: hostedclusterv1alpha1.ProxyServerSpec{
			Backends: []hostedclusterv1alpha1.ProxyBackend{
				{
					Name:            "backend",
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

	ctx := context.Background()
	err = xs.UpdateProxyConfig(ctx, proxy)
	require.NoError(t, err)

	// Verify proxy exists
	xs.mu.RLock()
	_, exists := xs.proxies[proxy.Name]
	xs.mu.RUnlock()
	assert.True(t, exists)

	// Remove proxy
	xs.RemoveProxyConfig(ctx, proxy.Name)

	// Verify proxy is removed
	xs.mu.RLock()
	_, exists = xs.proxies[proxy.Name]
	xs.mu.RUnlock()
	assert.False(t, exists, "proxy should be removed")
}

func TestXDSServer_WatchProxyServers(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, hostedclusterv1alpha1.AddToScheme(scheme))

	tests := []struct {
		name            string
		existingProxies []*hostedclusterv1alpha1.ProxyServer
		namespace       string
		wantErr         bool
		wantCount       int
		description     string
	}{
		{
			name:            "no existing proxies",
			existingProxies: nil,
			namespace:       "default",
			wantErr:         false,
			wantCount:       0,
			description:     "should handle empty namespace",
		},
		{
			name: "single proxy",
			existingProxies: []*hostedclusterv1alpha1.ProxyServer{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "proxy1",
						Namespace: "default",
					},
					Spec: hostedclusterv1alpha1.ProxyServerSpec{
						Backends: []hostedclusterv1alpha1.ProxyBackend{
							{
								Name:            "backend",
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
				},
			},
			namespace:   "default",
			wantErr:     false,
			wantCount:   1,
			description: "should initialize xDS for single proxy",
		},
		{
			name: "multiple proxies",
			existingProxies: []*hostedclusterv1alpha1.ProxyServer{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "proxy1",
						Namespace: "default",
					},
					Spec: hostedclusterv1alpha1.ProxyServerSpec{
						Backends: []hostedclusterv1alpha1.ProxyBackend{
							{
								Name:            "backend1",
								Hostname:        "test1.example.com",
								Port:            443,
								TargetService:   "test-service1",
								TargetPort:      443,
								TargetNamespace: "default",
								Protocol:        "TCP",
								TimeoutSeconds:  30,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "proxy2",
						Namespace: "default",
					},
					Spec: hostedclusterv1alpha1.ProxyServerSpec{
						Backends: []hostedclusterv1alpha1.ProxyBackend{
							{
								Name:            "backend2",
								Hostname:        "test2.example.com",
								Port:            443,
								TargetService:   "test-service2",
								TargetPort:      443,
								TargetNamespace: "default",
								Protocol:        "TCP",
								TimeoutSeconds:  30,
							},
						},
					},
				},
			},
			namespace:   "default",
			wantErr:     false,
			wantCount:   2,
			description: "should initialize xDS for multiple proxies",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with existing proxies
			var objects []client.Object
			for _, proxy := range tt.existingProxies {
				objects = append(objects, proxy)
			}

			k8sClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			xs, err := NewXDSServer(k8sClient, 0) // Use dynamic port allocation
			require.NoError(t, err)
			defer xs.Stop()

			ctx := context.Background()
			err = xs.WatchProxyServers(ctx, tt.namespace)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Verify proxies are loaded
			xs.mu.RLock()
			actualCount := len(xs.proxies)
			xs.mu.RUnlock()

			assert.Equal(t, tt.wantCount, actualCount, "unexpected number of proxies loaded")
		})
	}
}

func TestXDSServer_Stop(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, hostedclusterv1alpha1.AddToScheme(scheme))

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	xs, err := NewXDSServer(k8sClient, 0) // Use dynamic port allocation
	require.NoError(t, err)
	require.NotNil(t, xs.grpcServer)

	// Stop should not panic
	assert.NotPanics(t, func() {
		xs.Stop()
	})

	// Allow time for graceful shutdown
	time.Sleep(100 * time.Millisecond)

	// Calling stop again should not panic
	assert.NotPanics(t, func() {
		xs.Stop()
	})
}

func TestXDSServer_ConcurrentUpdates(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, hostedclusterv1alpha1.AddToScheme(scheme))

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	xs, err := NewXDSServer(k8sClient, 0) // Use dynamic port allocation
	require.NoError(t, err)
	defer xs.Stop()

	ctx := context.Background()
	done := make(chan bool)
	concurrentUpdates := 10

	// Perform concurrent updates
	for i := 0; i < concurrentUpdates; i++ {
		go func() {
			proxy := &hostedclusterv1alpha1.ProxyServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-proxy",
					Namespace: "default",
				},
				Spec: hostedclusterv1alpha1.ProxyServerSpec{
					Backends: []hostedclusterv1alpha1.ProxyBackend{
						{
							Name:            "backend",
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

			err := xs.UpdateProxyConfig(ctx, proxy)
			assert.NoError(t, err)
			done <- true
		}()
	}

	// Wait for all updates to complete
	for i := 0; i < concurrentUpdates; i++ {
		<-done
	}

	// Verify final state
	xs.mu.RLock()
	_, exists := xs.proxies["test-proxy"]
	version := xs.snapVersion
	xs.mu.RUnlock()

	assert.True(t, exists, "proxy should exist after concurrent updates")
	assert.Equal(t, concurrentUpdates, version, "version should equal number of updates")
}

func TestXDSServer_EmptyBackends(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, hostedclusterv1alpha1.AddToScheme(scheme))

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	xs := &XDSServer{
		client:  k8sClient,
		proxies: make(map[string]*hostedclusterv1alpha1.ProxyServer),
	}

	proxy := &hostedclusterv1alpha1.ProxyServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-proxy",
			Namespace: "default",
		},
		Spec: hostedclusterv1alpha1.ProxyServerSpec{
			Backends: []hostedclusterv1alpha1.ProxyBackend{},
		},
	}

	listeners, clusters, err := xs.buildEnvoyResources(proxy)
	require.NoError(t, err)
	assert.Empty(t, listeners, "should have no listeners with empty backends")
	assert.Empty(t, clusters, "should have no clusters with empty backends")
}
