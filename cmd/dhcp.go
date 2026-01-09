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
	"os"

	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/cldmnky/oooi/internal/dhcp"
)

var (
	dhcpConfigFile string
)

func init() {
	// Add flags to the dhcp command
	dhcpCmd.Flags().StringVar(&dhcpConfigFile, "config-file", "/etc/dhcp/oooi-dhcp.yaml",
		"Path to the DHCP server configuration file")
}

var dhcpCmd = &cobra.Command{
	Use:   "dhcp",
	Short: "Start the DHCP server",
	Long: `Starts the oooi DHCP server with KubeVirt integration. The server
provides DHCP services to virtual machines running on isolated secondary
networks (VLANs) that don't have direct access to the management cluster's
pod network.`,
	Run: runDHCP,
}

func runDHCP(cmd *cobra.Command, args []string) {
	log := ctrl.Log.WithName("dhcp")
	log.Info("starting DHCP server", "config-file", dhcpConfigFile)

	config := dhcp.NewConfig(dhcpConfigFile)
	if err := dhcp.Run(config); err != nil {
		log.Error(err, "failed to run DHCP server")
		os.Exit(1)
	}
}
