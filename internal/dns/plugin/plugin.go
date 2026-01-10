// Package plugin registers CoreDNS plugins for oooi DNS server
package plugin

import (
	// Core server components
	_ "github.com/coredns/coredns/core/dnsserver"

	// Essential plugins for split-horizon DNS
	_ "github.com/coredns/coredns/plugin/bind"    // Network interface binding
	_ "github.com/coredns/coredns/plugin/cache"   // Response caching
	_ "github.com/coredns/coredns/plugin/errors"  // Error logging
	_ "github.com/coredns/coredns/plugin/forward" // Upstream DNS forwarding
	_ "github.com/coredns/coredns/plugin/health"  // Health endpoint
	_ "github.com/coredns/coredns/plugin/hosts"   // Static A/AAAA records for HCP endpoints
	_ "github.com/coredns/coredns/plugin/log"     // Query logging
	_ "github.com/coredns/coredns/plugin/ready"   // Health check endpoint
	_ "github.com/coredns/coredns/plugin/reload"  // Auto-reload on config changes
	_ "github.com/coredns/coredns/plugin/whoami"  // Debugging

	// Additional useful plugins
	_ "github.com/coredns/coredns/plugin/acl"
	_ "github.com/coredns/coredns/plugin/any"
	_ "github.com/coredns/coredns/plugin/auto"
	_ "github.com/coredns/coredns/plugin/autopath"
	_ "github.com/coredns/coredns/plugin/bufsize"
	_ "github.com/coredns/coredns/plugin/cancel"
	_ "github.com/coredns/coredns/plugin/chaos"
	_ "github.com/coredns/coredns/plugin/debug"
	_ "github.com/coredns/coredns/plugin/dns64"
	_ "github.com/coredns/coredns/plugin/dnssec"
	_ "github.com/coredns/coredns/plugin/dnstap"
	_ "github.com/coredns/coredns/plugin/file"
	_ "github.com/coredns/coredns/plugin/grpc"
	_ "github.com/coredns/coredns/plugin/header"
	_ "github.com/coredns/coredns/plugin/loadbalance"
	_ "github.com/coredns/coredns/plugin/local"
	_ "github.com/coredns/coredns/plugin/loop"
	_ "github.com/coredns/coredns/plugin/metadata"
	_ "github.com/coredns/coredns/plugin/metrics"
	_ "github.com/coredns/coredns/plugin/minimal"
	_ "github.com/coredns/coredns/plugin/nsid"
	_ "github.com/coredns/coredns/plugin/pprof"
	_ "github.com/coredns/coredns/plugin/rewrite"
	_ "github.com/coredns/coredns/plugin/root"
	_ "github.com/coredns/coredns/plugin/template"
	_ "github.com/coredns/coredns/plugin/tls"
	_ "github.com/coredns/coredns/plugin/trace"
	_ "github.com/coredns/coredns/plugin/transfer"
	_ "github.com/coredns/coredns/plugin/view"
)
