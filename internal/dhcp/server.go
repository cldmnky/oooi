package dhcp

import (
	dhcpconfig "github.com/coredhcp/coredhcp/config"
	dhcplogger "github.com/coredhcp/coredhcp/logger"
	dhcpplugins "github.com/coredhcp/coredhcp/plugins"
	pl_dns "github.com/coredhcp/coredhcp/plugins/dns"
	pl_mtu "github.com/coredhcp/coredhcp/plugins/mtu"
	pl_nbp "github.com/coredhcp/coredhcp/plugins/nbp"
	pl_netmask "github.com/coredhcp/coredhcp/plugins/netmask"
	pl_prefix "github.com/coredhcp/coredhcp/plugins/prefix"
	pl_router "github.com/coredhcp/coredhcp/plugins/router"
	pl_searchdomains "github.com/coredhcp/coredhcp/plugins/searchdomains"
	pl_serverid "github.com/coredhcp/coredhcp/plugins/serverid"
	pl_sleep "github.com/coredhcp/coredhcp/plugins/sleep"
	pl_staticroute "github.com/coredhcp/coredhcp/plugins/staticroute"
	dhcpserver "github.com/coredhcp/coredhcp/server"

	pl_kubevirt "github.com/cldmnky/oooi/internal/dhcp/plugins/kubevirt"
	pl_leasedb "github.com/cldmnky/oooi/internal/dhcp/plugins/leasedb"
)

var plugins = []*dhcpplugins.Plugin{
	&pl_dns.Plugin,
	&pl_mtu.Plugin,
	&pl_netmask.Plugin,
	&pl_nbp.Plugin,
	&pl_prefix.Plugin,
	&pl_router.Plugin,
	&pl_serverid.Plugin,
	&pl_searchdomains.Plugin,
	&pl_sleep.Plugin,
	&pl_staticroute.Plugin,
	&pl_kubevirt.Plugin,
	&pl_leasedb.Plugin, // leasedb masquerades as range
}

func Run(config *Config) error {
	log := dhcplogger.GetLogger("main")
	log.WithField("config", *config.ConfigFile).Info("starting server")
	cfg, err := dhcpconfig.Load(*config.ConfigFile)
	if err != nil {
		log.WithError(err).Error("failed to load config")
		return err
	}

	// Register plugins
	for _, p := range plugins {
		log.WithField("plugin", p.Name).Debug("registering plugin")
		if err := dhcpplugins.RegisterPlugin(p); err != nil {
			log.WithError(err).Error("failed to register plugin")
			return err
		}
	}
	srv, err := dhcpserver.Start(cfg)
	if err != nil {
		log.WithError(err).Error("failed to start server")
		return err
	}
	if err := srv.Wait(); err != nil {
		log.WithError(err).Error("failed to wait for server")
		return err
	}
	return nil
}
