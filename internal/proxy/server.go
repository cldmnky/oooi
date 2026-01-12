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

	accesslog "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v3"
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	file_access_log "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/file/v3"
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

	// Update cache with proxy name as node ID
	if err := xs.cache.SetSnapshot(ctx, proxy.Name, snapshot); err != nil {
		log.Error(err, "failed to set snapshot", "proxy", proxy.Name)
		return err
	}

	log.Info("updated proxy configuration", "proxy", proxy.Name, "backends", len(proxy.Spec.Backends), "version", xs.snapVersion)
	return nil
}

// buildEnvoyResources builds Envoy listeners and clusters from ProxyServer backends
func (xs *XDSServer) buildEnvoyResources(proxy *hostedclusterv1alpha1.ProxyServer) ([]types.Resource, []types.Resource, error) {
	var clusters []types.Resource

	// Group backends by port
	portBackends := make(map[int32][]*hostedclusterv1alpha1.ProxyBackend)
	for i := range proxy.Spec.Backends {
		backend := &proxy.Spec.Backends[i]
		portBackends[backend.Port] = append(portBackends[backend.Port], backend)
	}
	listeners := make([]types.Resource, 0, len(portBackends))
	clusters = make([]types.Resource, 0, len(proxy.Spec.Backends))

	// Create listener for each unique port
	for port, backends := range portBackends {
		// Build filter chains for SNI routing
		var filterChains []*listener.FilterChain

		// Track potential fallback cluster for IP-based TLS (no SNI)
		// Fallback should route to konnectivity-server to establish tunnels
		var fallbackClusterName string

		// Port 6443 is used exclusively for kube-apiserver, so use plain TCP proxying
		// without SNI/TLS inspection. This allows HAProxy health checks (plain HTTP)
		// to reach the backend and get rejected gracefully by kube-apiserver rather
		// than failing at the proxy level.
		usePlainTCP := port == 6443

		// For plain TCP ports, we'll create a single catch-all filter chain
		// after processing all backends, so track the primary cluster name
		var plainTCPCluster string

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

			if usePlainTCP {
				// For plain TCP ports, only track the primary cluster (first backend)
				// We'll create a single catch-all filter chain after processing all backends
				if plainTCPCluster == "" {
					plainTCPCluster = clusterName
				}
			} else {
				// For other ports (443), use SNI-based routing
				// Create filter chain with SNI match
				// Include both primary hostname and any alternate hostnames
				serverNames := []string{backend.Hostname}
				serverNames = append(serverNames, backend.AlternateHostnames...)

				filterChain := &listener.FilterChain{
					FilterChainMatch: &listener.FilterChainMatch{
						ServerNames:       serverNames,
						TransportProtocol: "tls", // Require TLS with SNI
					},
					Filters: []*listener.Filter{{
						Name: wellknown.TCPProxy,
						ConfigType: &listener.Filter_TypedConfig{
							TypedConfig: tcpProxyAny,
						},
					}},
				}
				filterChains = append(filterChains, filterChain)

				// Determine fallback cluster for IP-based TLS connections (e.g., 172.5.0.1:443)
				// Fallback to konnectivity-server on port 443 so agents can connect
				if port == 443 && backend.TargetService == "konnectivity-server" {
					// Choose konnectivity-server cluster as fallback
					fallbackClusterName = clusterName
				}
			}
		}

		// For plain TCP ports (e.g., 6443), create a single catch-all filter chain
		// that routes to the primary cluster. This avoids duplicate matcher errors.
		if plainTCPCluster != "" {
			plainTCP := &tcp_proxy.TcpProxy{
				StatPrefix: "plain-tcp",
				ClusterSpecifier: &tcp_proxy.TcpProxy_Cluster{
					Cluster: plainTCPCluster,
				},
			}
			plainTCPAny, err := anypb.New(plainTCP)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to marshal plain tcp_proxy: %w", err)
			}

			plainTCPChain := &listener.FilterChain{
				FilterChainMatch: nil, // nil match = catch-all for plain TCP
				Filters: []*listener.Filter{{
					Name: wellknown.TCPProxy,
					ConfigType: &listener.Filter_TypedConfig{
						TypedConfig: plainTCPAny,
					},
				}},
			}
			filterChains = append(filterChains, plainTCPChain)
		}

		// Add a default filter chain without SNI match for IP-based TLS on 443
		// This catches clients that connect directly to the ClusterIP by IP (no hostname/SNI)
		// Must be added LAST so it acts as the default/fallback after SNI-based chains
		if fallbackClusterName != "" {
			fallbackTCP := &tcp_proxy.TcpProxy{
				StatPrefix: "fallback",
				ClusterSpecifier: &tcp_proxy.TcpProxy_Cluster{
					Cluster: fallbackClusterName,
				},
			}
			fallbackAny, err := anypb.New(fallbackTCP)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to marshal fallback tcp_proxy: %w", err)
			}

			// Create fallback chain for TLS connections without SNI
			// Use nil FilterChainMatch to create a true catch-all filter chain
			// This will match any connection that doesn't match the SNI-based chains
			fallbackChain := &listener.FilterChain{
				FilterChainMatch: nil, // nil match = catch-all
				Filters: []*listener.Filter{{
					Name: wellknown.TCPProxy,
					ConfigType: &listener.Filter_TypedConfig{
						TypedConfig: fallbackAny,
					},
				}},
			}
			filterChains = append(filterChains, fallbackChain)
		}

		// Create access log configuration with detailed connection metadata
		accessLogConfig := &file_access_log.FileAccessLog{
			Path: "/dev/stdout",
			AccessLogFormat: &file_access_log.FileAccessLog_LogFormat{
				LogFormat: &core.SubstitutionFormatString{
					Format: &core.SubstitutionFormatString_TextFormatSource{
						TextFormatSource: &core.DataSource{
							Specifier: &core.DataSource_InlineString{
								InlineString: "[%START_TIME%] %DOWNSTREAM_REMOTE_ADDRESS% → %UPSTREAM_CLUSTER% | SNI: %REQUESTED_SERVER_NAME% | TLS: %DOWNSTREAM_TLS_VERSION% %DOWNSTREAM_TLS_CIPHER% | Protocol: %PROTOCOL% | Flags: %RESPONSE_FLAGS% | Bytes: %BYTES_SENT%/%BYTES_RECEIVED% | ConnID: %CONNECTION_ID%\n",
							},
						},
					},
				},
			},
		}
		accessLogAny, err := anypb.New(accessLogConfig)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal access_log: %w", err)
		}

		// Create listener - use TLS inspector only for SNI-based ports (443)
		// Port 6443 uses plain TCP passthrough
		var listenerFilters []*listener.ListenerFilter
		if !usePlainTCP {
			// Create TLS inspector listener filter for SNI-based routing on port 443
			tlsInspector := &tls_inspector.TlsInspector{}
			tlsInspectorAny, err := anypb.New(tlsInspector)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to marshal tls_inspector: %w", err)
			}
			listenerFilters = []*listener.ListenerFilter{{
				Name: wellknown.TlsInspector,
				ConfigType: &listener.ListenerFilter_TypedConfig{
					TypedConfig: tlsInspectorAny,
				},
			}}
		}

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
			FilterChains:    filterChains,
			ListenerFilters: listenerFilters, // TLS inspector only for SNI ports
			AccessLog: []*accesslog.AccessLog{{
				Name: wellknown.FileAccessLog,
				ConfigType: &accesslog.AccessLog_TypedConfig{
					TypedConfig: accessLogAny,
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
