# oooi - OpenShift Hosted Control Plane Infrastructure Operator

[![Go Report Card](https://goreportcard.com/badge/github.com/cldmnky/oooi)](https://goreportcard.com/report/github.com/cldmnky/oooi)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

Kubernetes operator for deploying infrastructure components required by OpenShift Hosted Control Planes (HCP) running on OpenShift Virtualization with isolated secondary networks (VLANs).

## Overview

When KubeVirt VMs run with `attach-default-network: false` on isolated VLANs, they lack direct connectivity to the hosted control plane services running on the management cluster's pod network. This operator bridges that gap by deploying infrastructure services (DHCP, DNS, L4 proxy) onto the secondary network.

## Architecture

### Core Problem
OpenShift Hosted Control Planes decouple the control plane from worker nodes, enabling higher density and better resource utilization. However, when worker nodes are KubeVirt VMs running on isolated VLANs with `attach-default-network: false`, they lose direct connectivity to the hosted control plane services that run on the management cluster's pod network.

In traditional setups, this connectivity gap is bridged through:
- **Routes**: Exposing control plane services via OpenShift routes
- **L4 Load Balancers**: Using service load balancers to forward traffic

While functional, these approaches create security concerns in air-gapped or high-security environments, as they require tenant workloads to access the management cluster infrastructure.

### Solution
`oooi` addresses this by deploying the essential infrastructure services **directly onto the secondary network (VLAN)**, eliminating the need for tenant clusters to route traffic to or access the hosting cluster. This maintains strict network isolation while ensuring seamless connectivity to hosted control plane services.

The oooi operator watches for `HostedCluster` resources and automatically provisions:
- **DHCP Server**: Provides IP addresses and network configuration to VMs on the VLAN
- **DNS Server**: Resolves internal cluster DNS queries using split-horizon DNS
- **L4 Proxy (Envoy)**: Forwards traffic from the VLAN to control plane services

### Key Components
- **Infra CRD** (`api/v1alpha1/infra_types.go`): Custom resource defining infrastructure requirements
- **InfraReconciler** (`internal/controller/infra_controller.go`): Controller that provisions DHCP/DNS/Envoy services
- **API Group**: `hostedcluster.densityops.com/v1alpha1`

## Features

- 🚀 **Automatic Provisioning**: Deploys infrastructure when HostedCluster resources are created
- 🔒 **Network Isolation**: Supports air-gapped VLAN environments
- 🏗️ **Infrastructure as Code**: Declarative configuration via Kubernetes CRDs
- 🧹 **Garbage Collection**: Automatic cleanup when HostedClusters are deleted
- 📊 **Observability**: Metrics and logging integration
- 🧪 **Comprehensive Testing**: Unit tests with envtest, E2E tests with Kind

## Installation

### Prerequisites
- Go 1.24+
- `kubectl` or `oc` CLI
- Access to an OpenShift cluster with OpenShift Virtualization

### Quick Install
```bash
# Install CRDs
make install

# Deploy the operator
make deploy IMG=quay.io/cldmnky/oooi:v0.0.1
```

### From Source
```bash
# Clone the repository
git clone https://github.com/cldmnky/oooi.git
cd oooi

# Build and push the image
make container-build
make container-push IMG=your-registry/oooi:dev

# Deploy
make deploy IMG=your-registry/oooi:dev
```

## Usage

### Creating Infrastructure
The operator automatically watches for `HostedCluster` resources. When a cluster is created with the appropriate annotations, the operator provisions the required infrastructure.

Example HostedCluster spec:
```yaml
apiVersion: hypershift.openshift.io/v1beta1
kind: HostedCluster
metadata:
  name: example-cluster
  namespace: clusters
spec:
  # ... cluster configuration
  infra:
    networkType: OVNKubernetes
    # Additional configuration for VLAN networking
```

### Configuration
The operator uses the `Infra` custom resource to configure infrastructure components:

```yaml
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: Infra
metadata:
  name: example-infra
  namespace: clusters
spec:
  networkConfig:
    cidr: "192.168.100.0/24"
    gateway: "192.168.100.1"
    networkAttachmentDefinition: "vlan100"
    networkAttachmentNamespace: "default"
    dnsServers:  # Upstream DNS servers for CoreDNS forwarding
      - "8.8.8.8"
      - "8.8.4.4"
  
  infraComponents:
    # DHCP Server Configuration
    dhcp:
      enabled: true
      serverIP: "192.168.100.2"
      rangeStart: "192.168.100.100"
      rangeEnd: "192.168.100.200"
      leaseTime: "1h"
    
    # DNS Server Configuration
    dns:
      enabled: true
      serverIP: "192.168.100.3"
      baseDomain: "example.com"
      clusterName: "my-cluster"
    
    # Proxy Server Configuration
    proxy:
      enabled: true
      serverIP: "192.168.100.10"
      controlPlaneNamespace: "clusters-my-cluster"
```

**Key Configuration Points**:

- **`networkConfig.dnsServers`**: Upstream DNS servers that CoreDNS forwards non-HCP queries to
- **`infraComponents.dns.enabled: true`**: When enabled, DHCP automatically uses the DNS server IP
- **DNS Flow**: VMs → DHCP assigns CoreDNS IP → CoreDNS resolves HCP domains → CoreDNS forwards other queries to upstream

See [DNS_SETUP.md](docs/DNS_SETUP.md) for detailed DNS configuration and [PROXY_SETUP.md](docs/PROXY_SETUP.md) for proxy configuration.

## Development

### Prerequisites
- Go 1.24+
- `kubectl` or `oc` CLI
- `kind` (for E2E testing)
- Docker/Podman

### Development Workflow
```bash
# Regenerate CRDs and deepcopy after API changes
make manifests generate

# Format and vet code
make fmt vet

# Run unit tests
make test

# Run E2E tests
make test-e2e

# Lint code
make lint
```

### Local Testing
```bash
# Install CRDs to cluster
make install

# Run controller locally
make run

# In another terminal, create test resources
kubectl apply -f config/samples/
```

### Building
```bash
# Build container image
make container-build

# Build binary
make build
```

## Testing

### Unit Tests
```bash
make test
```
Runs unit tests using envtest with a local etcd/apiserver.

### E2E Tests
```bash
# Setup Kind cluster with required components
make setup-test-e2e

# Run E2E tests
make test-e2e

# Cleanup
make cleanup-test-e2e
```

### Test Coverage
- Controller reconciliation logic
- RBAC permissions
- Resource creation and cleanup
- Error handling and edge cases

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Follow TDD: Write tests first, then implement
4. Run `make test` and `make lint`
5. Commit your changes (`git commit -m 'Add amazing feature'`)
6. Push to the branch (`git push origin feature/amazing-feature`)
7. Open a Pull Request

### Code Standards
- Follow Go conventions (`gofmt`, `go vet`)
- Pass `golangci-lint` checks
- Write comprehensive tests
- Use structured logging with `logf.FromContext(ctx)`
- Add RBAC markers for new resource access

## Documentation

- [Architecture Design](PLAN.md) - Comprehensive technical design document
- [E2E Testing Guide](QUICKSTART_E2E.md) - Quick start for end-to-end testing
- [DNS Setup](docs/DNS_SETUP.md) - DNS configuration details
- [Proxy Setup](docs/PROXY_SETUP.md) - Proxy configuration details
- [Static IPAM](docs/STATIC_IPAM.md) - IP address management

## License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) for details.

## Support

- Issues: [GitHub Issues](https://github.com/cldmnky/oooi/issues)
- Discussions: [GitHub Discussions](https://github.com/cldmnky/oooi/discussions)

---

Built with ❤️ using [Kubebuilder](https://kubebuilder.io/) and [Operator SDK](https://sdk.operatorframework.io/).
