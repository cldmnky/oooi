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
	"sync"

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	hostedclusterv1alpha1 "github.com/cldmnky/oooi/api/v1alpha1"
)

// XDSServer manages the Envoy configuration via xDS protocol
// This is a simplified implementation that manages ProxyServer state
// The actual xDS protocol implementation would be added in a future phase
type XDSServer struct {
	client client.Client
	mu     sync.RWMutex
	// Track ProxyServers that need configuration
	proxies map[string]*hostedclusterv1alpha1.ProxyServer
}

// NewXDSServer creates a new xDS server
func NewXDSServer(k8sClient client.Client) (*XDSServer, error) {
	xs := &XDSServer{
		client:  k8sClient,
		proxies: make(map[string]*hostedclusterv1alpha1.ProxyServer),
	}
	return xs, nil
}

// UpdateProxyConfig updates the xDS configuration for a specific proxy
func (xs *XDSServer) UpdateProxyConfig(ctx context.Context, proxy *hostedclusterv1alpha1.ProxyServer) error {
	log := logf.FromContext(ctx)
	xs.mu.Lock()
	defer xs.mu.Unlock()

	xs.proxies[proxy.Name] = proxy

	log.Info("updated proxy configuration", "proxy", proxy.Name, "backends", len(proxy.Spec.Backends))
	return nil
}

// RemoveProxyConfig removes the xDS configuration for a specific proxy
func (xs *XDSServer) RemoveProxyConfig(ctx context.Context, proxyName string) {
	log := logf.FromContext(ctx)
	xs.mu.Lock()
	defer xs.mu.Unlock()

	delete(xs.proxies, proxyName)
	log.Info("removed proxy configuration", "proxy", proxyName)
}

// Stop stops the xDS server
func (xs *XDSServer) Stop() {
	// No-op for now; full gRPC server would be implemented here
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
	for _, proxy := range proxyList.Items {
		proxyPtr := proxy // Create a copy for the goroutine
		if err := xs.UpdateProxyConfig(ctx, &proxyPtr); err != nil {
			log.Error(err, "failed to update proxy config", "proxy", proxy.Name)
		}
	}

	log.Info("initialized xDS configuration", "proxies", len(proxyList.Items))
	return nil
}
