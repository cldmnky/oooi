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

package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	hostedclusterv1alpha1 "github.com/cldmnky/oooi/api/v1alpha1"
	"github.com/cldmnky/oooi/internal/proxy"
)

var (
	proxyXDSPort     int32
	proxyNamespace   string
	proxyName        string
	proxyLogLevel    string
	proxyMetricsPort int32
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(hostedclusterv1alpha1.AddToScheme(scheme))
}

// proxyCmd represents the proxy command
var proxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "Start the xDS control plane for Envoy proxy",
	Long: `Start the xDS control plane that manages Envoy proxy configuration.
	
This command runs the Envoy xDS server that watches ProxyServer resources and 
dynamically configures Envoy with SNI-based routing to backend services.`,
	RunE: runProxy,
}

func init() {
	rootCmd.AddCommand(proxyCmd)

	proxyCmd.Flags().Int32Var(&proxyXDSPort, "xds-port", 18000,
		"gRPC port for xDS communication with Envoy")
	proxyCmd.Flags().StringVar(&proxyNamespace, "namespace", "default",
		"Namespace to watch for ProxyServer resources")
	proxyCmd.Flags().StringVar(&proxyName, "proxy-name", "",
		"Name of the ProxyServer resource to manage (empty = watch all in namespace)")
	proxyCmd.Flags().StringVar(&proxyLogLevel, "proxy-log-level", "info",
		"Log level for the xDS server (trace|debug|info|warning|error|critical)")
	proxyCmd.Flags().Int32Var(&proxyMetricsPort, "metrics-port", 8080,
		"Port for metrics endpoint")
}

func runProxy(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup logging
	opts := zap.Options{
		Development: true,
	}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("proxy")

	log.Info("starting proxy xDS control plane",
		"xds-port", proxyXDSPort,
		"namespace", proxyNamespace,
		"metrics-port", proxyMetricsPort)

	// Create Kubernetes client
	config, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	k8sClient, err := ctrl.NewClient(ctrl.Options{
		Scheme: scheme,
		Config: config,
	})
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Create xDS server
	xdsServer, err := proxy.NewXDSServer(k8sClient, proxyXDSPort)
	if err != nil {
		return fmt.Errorf("failed to create xDS server: %w", err)
	}
	defer xdsServer.Stop()

	log.Info("xDS server created and listening", "port", proxyXDSPort)

	// Watch ProxyServer resources
	if err := xdsServer.WatchProxyServers(ctx, proxyNamespace); err != nil {
		return fmt.Errorf("failed to watch proxy servers: %w", err)
	}

	// Setup signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	log.Info("proxy control plane ready, waiting for signals")

	// Wait for shutdown signal
	<-sigCh
	log.Info("shutting down proxy control plane")

	return nil
}
