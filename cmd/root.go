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
	"flag"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	cfgFile string
	rootCmd = &cobra.Command{
		Use:   "oooi",
		Short: "OpenShift Hosted Control Plane Infrastructure Operator",
		Long: `oooi is a Kubernetes operator for deploying infrastructure components 
required by OpenShift Hosted Control Planes (HCP) running on OpenShift 
Virtualization with isolated secondary networks (VLANs).`,
	}
)

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.oooi.yaml)")
	viper.BindPFlag("config", rootCmd.PersistentFlags().Lookup("config"))

	// Add zap flags for logging
	zapfs := flag.NewFlagSet("zap", flag.ExitOnError)
	opts := &zap.Options{
		Development: true,
	}
	opts.BindFlags(zapfs)
	rootCmd.PersistentFlags().AddGoFlagSet(zapfs)

	// Set logger with default options
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(opts)))

	// Add subcommands
	rootCmd.AddCommand(managerCmd)
	rootCmd.AddCommand(dhcpCmd)
}

func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".oooi")
	}
	viper.AutomaticEnv()
	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}
}
