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

	"github.com/cldmnky/oooi/internal/dns"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	corefilePath string
)

// dnsCmd represents the dns subcommand that runs a CoreDNS server
var dnsCmd = &cobra.Command{
	Use:   "dns",
	Short: "Run the CoreDNS-based DNS server",
	Long: `Run a CoreDNS-based DNS server for providing split-horizon DNS resolution
to hosted control plane workloads on isolated VLAN networks.

The server loads its configuration from a Corefile and automatically reloads
when the configuration changes (if the reload plugin is configured).

Example Corefile:
  . {
      hosts {
          192.168.1.10 api.cluster.example.com
          192.168.1.10 api-int.cluster.example.com
          fallthrough
      }
      forward . 8.8.8.8 8.8.4.4
      reload 5s
      log
      errors
      ready :8181
  }
`,
	RunE: runDNS,
}

func init() {
	rootCmd.AddCommand(dnsCmd)

	// Flags
	dnsCmd.Flags().StringVarP(&corefilePath, "corefile", "c", "/etc/coredns/Corefile",
		"Path to the Corefile configuration")
}

func runDNS(cmd *cobra.Command, args []string) error {
	setupLog := ctrl.Log.WithName("dns")
	setupLog.Info("Starting DNS server", "corefile", corefilePath)

	// Create DNS server
	server, err := dns.NewServer(corefilePath)
	if err != nil {
		return fmt.Errorf("failed to create DNS server: %w", err)
	}

	// Setup context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		setupLog.Info("Received signal, shutting down", "signal", sig)
		cancel()
	}()

	// Start server
	setupLog.Info("DNS server starting")
	if err := server.Start(ctx); err != nil && err != context.Canceled {
		log.Log.Error(err, "DNS server failed")
		return err
	}

	setupLog.Info("DNS server stopped gracefully")
	return nil
}
