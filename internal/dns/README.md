# DNS Server Component

CoreDNS-based split-horizon DNS server for OpenShift Hosted Control Planes (HCP) on isolated VLAN networks.

## Features

- **CoreDNS Integration**: Full CoreDNS plugin support with explicit plugin registration
- **Hosts Plugin**: Critical for split-horizon DNS - maps HCP control plane FQDNs to Envoy L4 proxy IP
- **Automatic Reload**: Watches Corefile changes (requires `reload` plugin configured with >=2s interval)
- **Dual-Network Support**: Binds to both primary and secondary networks via Multus annotations
- **Split-Horizon DNS**: Routes HCP API endpoints to L4 proxy, forwards everything else upstream

## Usage

### As a Subcommand

```bash
oooi dns --corefile /etc/coredns/Corefile
```

### Example Corefile for HCP

```corefile
.:53 {
    # Static entries for HCP control plane (points to Envoy proxy)
    hosts {
        192.168.1.10 api.cluster.example.com
        192.168.1.10 api-int.cluster.example.com
        192.168.1.10 oauth-openshift.apps.cluster.example.com
        fallthrough
    }
    
    # Forward all other queries to upstream DNS
    forward . 8.8.8.8 8.8.4.4
    
    # Auto-reload configuration every 5 seconds
    reload 5s
    
    # Enable logging and error reporting
    log
    errors
    
    # Health check endpoint
    ready :8181
    health :8080
}
```

## Configuration from ConfigMap

In production, the Infra controller will create a ConfigMap containing the Corefile:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: coredns-config
  namespace: clusters-my-cluster
data:
  Corefile: |
    .:53 {
        hosts {
            192.168.1.10 api.my-cluster.example.com
            192.168.1.10 api-int.my-cluster.example.com
            192.168.1.10 oauth-openshift.apps.my-cluster.example.com
            fallthrough
        }
        forward . 8.8.8.8
        reload 2s
        log
        errors
    }
```

Mount this in the deployment:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: coredns
spec:
  template:
    spec:
      containers:
      - name: coredns
        image: quay.io/cldmnky/oooi:latest
        command: ["/oooi", "dns", "--corefile", "/etc/coredns/Corefile"]
        volumeMounts:
        - name: config
          mountPath: /etc/coredns
          readOnly: true
      volumes:
      - name: config
        configMap:
          name: coredns-config
```

## Testing

Run unit tests:

```bash
make test
```

Run DNS server tests specifically:

```bash
go test -v ./internal/dns/... -timeout 60s
```

## Architecture

1. **Plugin System**: Uses CoreDNS's plugin architecture for extensibility
2. **Reload Mechanism**: CoreDNS `reload` plugin watches for Corefile changes
3. **Graceful Shutdown**: Context-based cancellation ensures clean termination
4. **Multi-Network**: Designed for dual-homed deployments (pod network + secondary VLAN)

## Implementation Details

- **Package**: `internal/dns`
- **Main Types**:
  - `Server`: Wraps CoreDNS caddy instance with lifecycle management
  - `NewServer(corefilePath)`: Creates server instance
  - `Start(ctx)`: Starts server, blocks until context cancelled
  - `Stop()`: Gracefully shuts down server

## Development

The DNS server integrates with the operator workflow:

1. **InfraReconciler** creates/updates CoreDNS ConfigMap
2. **DNS Deployment** mounts ConfigMap and runs `oooi dns`
3. **Reload plugin** detects changes and reloads configuration
4. **HCP VMs** use DNS server for split-horizon resolution

## Plugins Used
### Essential Plugins (Explicitly Registered)

**Core Split-Horizon Plugins:**
- **`hosts`**: 🔑 **CRITICAL** - Static A/AAAA records for HCP control plane endpoints (api, api-int, oauth, apps)
- **`forward`**: Upstream DNS forwarding for non-HCP domains
- **`reload`**: Automatic config reload when ConfigMap changes

**Observability & Health:**
- `log`: Query logging for debugging
- `errors`: Error logging
- `ready`: Health check endpoint (e.g., :8181)
- `health`: Liveness probe endpoint
- `metrics`: Prometheus metrics exposure

**Performance & Behavior:**
- `cache`: Response caching to reduce upstream queries
- `bind`: Network interface binding (dual-homed setup)
- `loop`: Loop detection and prevention

**Debugging:**
- `whoami`: Returns client IP (useful for network troubleshooting)

### Additional Available Plugins

All standard CoreDNS plugins are registered and available:
- `acl`, `any`, `auto`, `autopath`, `bufsize`, `cancel`, `chaos`
- `debug`, `dns64`, `dnssec`, `dnstap`, `file`, `grpc`
- `header`, `loadbalance`, `local`, `metadata`, `minimal`
- `nsid`, `pprof`, `rewrite`, `root`, `template`, `tls`
- `trace`, `transfer`, `view`

See [CoreDNS Plugins Documentation](https://coredns.io/plugins/) for details.
- And more... (see [CoreDNS Plugins](https://coredns.io/plugins/))

## Next Steps

- [ ] Add metrics exposure for Prometheus
- [ ] Integrate with Multus NetworkAttachmentDefinition
- [ ] Generate Corefile from InfraSpec in controller
- [ ] Add DNSSEC support for production
- [ ] Implement custom plugin for dynamic HCP endpoint discovery
