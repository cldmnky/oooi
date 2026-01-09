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

// Package cmd implements the CLI interface for oooi operator.
// It provides subcommands for running the manager (controllers) and DHCP server.
package cmd

import (
	"flag"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	cfgFile string
	zapOpts *zap.Options
	rootCmd = &cobra.Command{
		Use:   "oooi",
		Short: "OpenShift Hosted Control Plane Infrastructure Operator",
		Long: `oooi is a Kubernetes operator for deploying infrastructure components 
required by OpenShift Hosted Control Planes (HCP) running on OpenShift 
Virtualization with isolated secondary networks (VLANs).`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Initialize logger after flags are parsed
			ctrl.SetLogger(zap.New(zap.UseFlagOptions(zapOpts)))
		},
	}
)

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.oooi.yaml)")
	_ = viper.BindPFlag("config", rootCmd.PersistentFlags().Lookup("config"))

	// Add zap flags for logging
	zapfs := flag.NewFlagSet("zap", flag.ExitOnError)
	zapOpts = &zap.Options{
		Development: true,
	}
	zapOpts.BindFlags(zapfs)
	rootCmd.PersistentFlags().AddGoFlagSet(zapfs)

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
		ctrl.Log.Info("loaded configuration", "config-file", viper.ConfigFileUsed())
	}
}
