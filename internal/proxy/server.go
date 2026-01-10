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
	"fmt"
	"net"
	"sync"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	tls_inspector "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/tls_inspector/v3"
	tcp_proxy "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	discoverygrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	hostedclusterv1alpha1 "github.com/cldmnky/oooi/api/v1alpha1"
)

const (
	nodeID = "proxy-node"
)

// XDSServer manages the Envoy configuration via xDS protocol using go-control-plane
type XDSServer struct {
	client      client.Client
	cache       cache.SnapshotCache
	grpcServer  *grpc.Server
	mu          sync.RWMutex
	proxies     map[string]*hostedclusterv1alpha1.ProxyServer
	snapVersion int
}

// NewXDSServer creates a new xDS server with go-control-plane
func NewXDSServer(k8sClient client.Client, xdsPort int32) (*XDSServer, error) {
	// Create snapshot cache
	snapshotCache := cache.NewSnapshotCache(false, cache.IDHash{}, nil)

	xs := &XDSServer{
		client:      k8sClient,
		cache:       snapshotCache,
		proxies:     make(map[string]*hostedclusterv1alpha1.ProxyServer),
		snapVersion: 0,
	}

	// Create xDS server
	srv := server.NewServer(context.Background(), snapshotCache, nil)

	// Start gRPC server
	grpcServer := grpc.NewServer()
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", xdsPort))
	if err != nil {
		return nil, fmt.Errorf("failed to listen on port %d: %w", xdsPort, err)
	}

	// Register xDS services
	discoverygrpc.RegisterAggregatedDiscoveryServiceServer(grpcServer, srv)

	xs.grpcServer = grpcServer

	// Start serving in background
	go func() {
		log := logf.FromContext(context.Background())
		log.Info("starting xDS gRPC server", "port", xdsPort)
		if err := grpcServer.Serve(lis); err != nil {
			log.Error(err, "xDS gRPC server failed")
		}
	}()

	return xs, nil
}

// UpdateProxyConfig updates the xDS configuration for a specific proxy
func (xs *XDSServer) UpdateProxyConfig(ctx context.Context, proxy *hostedclusterv1alpha1.ProxyServer) error {
	log := logf.FromContext(ctx)
	xs.mu.Lock()
	defer xs.mu.Unlock()

	xs.proxies[proxy.Name] = proxy
	xs.snapVersion++

	// Build Envoy configuration resources
	listeners, clusters, err := xs.buildEnvoyResources(proxy)
	if err != nil {
		log.Error(err, "failed to build Envoy resources", "proxy", proxy.Name)
		return err
	}

	// Create snapshot
	snapshot, err := cache.NewSnapshot(
		fmt.Sprintf("%d", xs.snapVersion),
		map[resource.Type][]types.Resource{
			resource.ClusterType:  clusters,
			resource.ListenerType: listeners,
		},
	)
	if err != nil {
		log.Error(err, "failed to create snapshot", "proxy", proxy.Name)
		return err
	}

	// Update cache
	if err := xs.cache.SetSnapshot(ctx, nodeID, snapshot); err != nil {
		log.Error(err, "failed to set snapshot", "proxy", proxy.Name)
		return err
	}

	log.Info("updated proxy configuration", "proxy", proxy.Name, "backends", len(proxy.Spec.Backends), "version", xs.snapVersion)
	return nil
}

// buildEnvoyResources builds Envoy listeners and clusters from ProxyServer backends
func (xs *XDSServer) buildEnvoyResources(proxy *hostedclusterv1alpha1.ProxyServer) ([]types.Resource, []types.Resource, error) {
	var listeners []types.Resource
	var clusters []types.Resource

	// Group backends by port
	portBackends := make(map[int32][]*hostedclusterv1alpha1.ProxyBackend)
	for i := range proxy.Spec.Backends {
		backend := &proxy.Spec.Backends[i]
		portBackends[backend.Port] = append(portBackends[backend.Port], backend)
	}

	// Create listener for each unique port
	for port, backends := range portBackends {
		// Build filter chains for SNI routing
		var filterChains []*listener.FilterChain

		for _, backend := range backends {
			// Create cluster for this backend
			clusterName := fmt.Sprintf("%s-%s", proxy.Name, backend.Name)
			targetAddr := fmt.Sprintf("%s.%s.svc.cluster.local", backend.TargetService, backend.TargetNamespace)

			clusterResource := &cluster.Cluster{
				Name:                 clusterName,
				ConnectTimeout:       durationpb.New(time.Duration(backend.TimeoutSeconds) * time.Second),
				ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_LOGICAL_DNS},
				LbPolicy:             cluster.Cluster_ROUND_ROBIN,
				LoadAssignment: &endpoint.ClusterLoadAssignment{
					ClusterName: clusterName,
					Endpoints: []*endpoint.LocalityLbEndpoints{{
						LbEndpoints: []*endpoint.LbEndpoint{{
							HostIdentifier: &endpoint.LbEndpoint_Endpoint{
								Endpoint: &endpoint.Endpoint{
									Address: &core.Address{
										Address: &core.Address_SocketAddress{
											SocketAddress: &core.SocketAddress{
												Protocol: core.SocketAddress_TCP,
												Address:  targetAddr,
												PortSpecifier: &core.SocketAddress_PortValue{
													PortValue: uint32(backend.TargetPort),
												},
											},
										},
									},
								},
							},
						}},
					}},
				},
				DnsLookupFamily: cluster.Cluster_V4_ONLY,
			}
			clusters = append(clusters, clusterResource)

			// Create TCP proxy filter
			tcpProxy := &tcp_proxy.TcpProxy{
				StatPrefix: backend.Name,
				ClusterSpecifier: &tcp_proxy.TcpProxy_Cluster{
					Cluster: clusterName,
				},
			}
			tcpProxyAny, err := anypb.New(tcpProxy)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to marshal tcp_proxy: %w", err)
			}

			// Create filter chain with SNI match
			filterChain := &listener.FilterChain{
				FilterChainMatch: &listener.FilterChainMatch{
					ServerNames: []string{backend.Hostname},
				},
				Filters: []*listener.Filter{{
					Name: wellknown.TCPProxy,
					ConfigType: &listener.Filter_TypedConfig{
						TypedConfig: tcpProxyAny,
					},
				}},
			}
			filterChains = append(filterChains, filterChain)
		}

		// Create TLS inspector listener filter for SNI
		tlsInspector := &tls_inspector.TlsInspector{}
		tlsInspectorAny, err := anypb.New(tlsInspector)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal tls_inspector: %w", err)
		}

		// Create listener
		listenerResource := &listener.Listener{
			Name: fmt.Sprintf("%s-listener-%d", proxy.Name, port),
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					SocketAddress: &core.SocketAddress{
						Protocol: core.SocketAddress_TCP,
						Address:  "0.0.0.0",
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: uint32(port),
						},
					},
				},
			},
			FilterChains: filterChains,
			ListenerFilters: []*listener.ListenerFilter{{
				Name: wellknown.TlsInspector,
				ConfigType: &listener.ListenerFilter_TypedConfig{
					TypedConfig: tlsInspectorAny,
				},
			}},
		}
		listeners = append(listeners, listenerResource)
	}

	return listeners, clusters, nil
}

// RemoveProxyConfig removes the xDS configuration for a specific proxy
func (xs *XDSServer) RemoveProxyConfig(ctx context.Context, proxyName string) {
	log := logf.FromContext(ctx)
	xs.mu.Lock()
	defer xs.mu.Unlock()

	delete(xs.proxies, proxyName)
	log.Info("removed proxy configuration", "proxy", proxyName)
}

// Stop stops the xDS gRPC server
func (xs *XDSServer) Stop() {
	if xs.grpcServer != nil {
		xs.grpcServer.GracefulStop()
	}
}

// WatchProxyServers watches for ProxyServer resources and updates xDS configuration
func (xs *XDSServer) WatchProxyServers(ctx context.Context, namespace string) error {
	log := logf.FromContext(ctx)

	// List existing ProxyServers in the namespace
	proxyList := &hostedclusterv1alpha1.ProxyServerList{}
	if err := xs.client.List(ctx, proxyList, client.InNamespace(namespace)); err != nil {
		log.Error(err, "failed to list ProxyServers")
		return err
	}

	// Update xDS for each ProxyServer
	for i := range proxyList.Items {
		proxy := &proxyList.Items[i]
		if err := xs.UpdateProxyConfig(ctx, proxy); err != nil {
			log.Error(err, "failed to update proxy config", "proxy", proxy.Name)
		}
	}

	log.Info("initialized xDS configuration", "proxies", len(proxyList.Items))
	return nil
}
