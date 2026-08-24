# oooi - OpenShift Hosted Control Plane Infrastructure Operator

[![Go Report Card](https://goreportcard.com/badge/github.com/cldmnky/oooi)](https://goreportcard.com/report/github.com/cldmnky/oooi)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

Kubernetes operator for deploying infrastructure components required by OpenShift Hosted Control Planes (HCP) running on OpenShift Virtualization with isolated secondary networks (VLANs).

## Overview

When KubeVirt VMs run with `attach-default-network: false` on isolated VLANs, they lack direct connectivity to the hosted control plane services running on the management cluster's pod network. This operator bridges that gap by deploying infrastructure services (DHCP, DNS, L4 proxy) onto the secondary network.

## Getting started

This walkthrough creates the HyperShift `HostedCluster` first and then applies the oooi `Infra` resource. oooi consumes an existing HostedCluster; it does not create one.

### Prerequisites

- An OpenShift management cluster with HyperShift, OpenShift Virtualization, Multus, and MetalLB operator access
- `kubectl` or `oc`, `hcp`, Go 1.24+, and access to push images to the configured registry
- A Multus NAD for the isolated network, such as `default/vlan203`
- HCP secrets in the HostedCluster namespace: pull secret, SSH key, and etcd encryption key
- A DHCP-disabled upstream network DHCP service for the VLAN, so oooi is the only DHCP server on that network

Confirm the management-cluster prerequisites before deploying:

```bash
kubectl get net-attach-def -n default vlan203
kubectl get secret -n clusters pullsecret-clusters sshkey-clusters etcd-encryption-key-clusters
```

### Build and deploy oooi

`make container-build` builds and pushes a multi-architecture image. Use the manifest digest printed by `ko` for both the operator and the component images. The checked-in species-8472 sample is pinned to the last validated digest.

```bash
make container-build

# Replace <digest> with the digest printed by the build.
export OOOI_IMAGE=quay.io/cldmnky/oooi@sha256:<digest>
make deploy IMG="$OOOI_IMAGE"
kubectl -n oooi-system rollout status deployment/oooi-controller-manager --timeout=5m
```

When using a newly built image, update the three oooi image fields in `config/samples/species-8472-infra.yaml` to the same digest before applying that sample.

### Create the HostedCluster

Apply the HostedCluster and NodePool manifest generated for the environment. For the lab used by this repository:

```bash
kubectl apply -f /path/to/home-lab/demos/multicluster/hosted-species-8472.yaml
```

The lab manifest targets OpenShift 4.22 and creates `species-8472` in namespace `clusters`, with three KubeVirt workers on `default/vlan203`, `attachDefaultNetwork: false`, and the HCP endpoints under `clusters.blahonga.me`.

### Apply Infra

Apply the declarative sample after the HostedCluster object exists. Do not wait for the HostedCluster to become Available before applying `Infra`; DHCP, DNS, and proxy services are needed while the data plane bootstraps. When apps ingress is enabled, oooi waits for a Ready hosted worker before installing MetalLB, so OLM can schedule the operator bundle unpack Job.

```bash
kubectl apply -f config/samples/species-8472-infra.yaml

kubectl -n clusters wait --for=condition=Available hostedcluster/species-8472 --timeout=30m
kubectl -n clusters wait --for=condition=Ready infra/species-8472 --timeout=30m
kubectl -n clusters get dhcpserver,dnsserver,proxyserver
kubectl -n clusters get infra species-8472
```

The validated sample uses VLAN `10.202.64.0/24` with gateway `10.202.64.1`, DHCP `10.202.64.2`, DNS `10.202.64.3`, proxy `10.202.64.4`, DHCP pool `10.202.64.200-10.202.64.254`, and apps VIP `10.202.64.180`. It forwards external DNS to the lab resolvers `10.201.0.2` and `10.201.0.1`. `internalProxyService` points to the proxy Service; oooi resolves that Service to its ClusterIP when generating the pod-network DNS view.

### Verify from the VLAN

Use a real VLAN client or a VLAN-attached probe, not only a management-cluster pod. The following names should resolve through `10.202.64.3`:

```bash
dig @10.202.64.3 api.species-8472.clusters.blahonga.me
dig @10.202.64.3 console-openshift-console.apps.species-8472.clusters.blahonga.me

curl -k https://api.species-8472.clusters.blahonga.me:6443/version
curl -k -o /dev/null -w '%{http_code}\n' \
  'https://oauth.species-8472.clusters.blahonga.me/oauth/authorize?client_id=openshift-challenging-client&response_type=token'
curl -k -o /dev/null -w '%{http_code}\n' \
  https://console-openshift-console.apps.species-8472.clusters.blahonga.me
```

Expected results are API `200`, unauthenticated OAuth `401`, and console `200`. Ignition normally returns `404` at `/`, and konnectivity returns `415` to a plain HTTP request; those statuses still prove that the TLS/SNI proxy path reached the intended backend. From the pod network, the same HCP and apps names should resolve to the proxy Service ClusterIP instead of the VLAN IP.

### Public DNS ownership

The `Infra` controller manages the hosted `oooi-ingress` Service, MetalLB VIP, VLAN DNS records, and proxy backends. It does not write public DNS records. The sample adds ExternalDNS labels and annotations, but a Route53 writer must run where it can watch the hosted cluster Service. Management-cluster ExternalDNS cannot see that Service. Without a public DNS writer, VLAN clients still work through oooi DNS; publish `*.apps.species-8472.clusters.blahonga.me` to the apps VIP when public resolution is required.

For this lab, the Route53 ExternalDNS operator watches the management cluster by default, so use a separate ExternalDNS Deployment with a hosted-cluster kubeconfig:

```bash
tmp=$(mktemp -d)
kubectl -n clusters get secret species-8472-admin-kubeconfig \
  -o jsonpath='{.data.kubeconfig}' | base64 -d > "$tmp/config"
sed -i.bak \
  's#server: https://api.species-8472.clusters.blahonga.me:6443#server: https://kube-apiserver.clusters-species-8472.svc.cluster.local:6443\n    tls-server-name: api.species-8472.clusters.blahonga.me#' \
  "$tmp/config"
rm -f "$tmp/config.bak"
kubectl -n external-dns-operator create secret generic species-8472-external-dns-kubeconfig \
  --from-file=config="$tmp/config" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n external-dns-operator create secret generic species-8472-external-dns-credentials \
  --from-file=credentials="$HOME/.aws/credentials" --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f config/samples/species-8472-external-dns.yaml
kubectl -n external-dns-operator rollout status deployment/species-8472-external-dns --timeout=5m
rm -rf "$tmp"
```

Use a read-only hosted-cluster ServiceAccount kubeconfig instead of the published admin kubeconfig in a shared environment. Verify convergence with `dig +short console-openshift-console.apps.species-8472.clusters.blahonga.me @1.1.1.1`; it should return the current `appsIngressStatus.externalIP`.

## Architecture

### Core Problem
OpenShift Hosted Control Planes decouple the control plane from worker nodes, enabling higher density and better resource utilization. However, when worker nodes are KubeVirt VMs running on isolated VLANs with `attach-default-network: false`, they lose direct connectivity to the hosted control plane services that run on the management cluster's pod network.

In traditional setups, this connectivity gap is bridged through:
- **Routes**: Exposing control plane services via OpenShift routes
- **L4 Load Balancers**: Using service load balancers to forward traffic

While functional, these approaches create security concerns in air-gapped or high-security environments, as they require tenant workloads to access the management cluster infrastructure.

### Solution
`oooi` addresses this by deploying the essential infrastructure services **directly onto the secondary network (VLAN)**, eliminating the need for tenant clusters to route traffic to or access the hosting cluster. This maintains strict network isolation while ensuring seamless connectivity to hosted control plane services.

The oooi operator is driven by a declarative `Infra` custom resource and automatically provisions:
- **DHCP Server**: Provides IP addresses and network configuration to VMs on the VLAN
- **DNS Server**: Resolves internal cluster DNS queries using split-horizon DNS
- **L4 Proxy (Envoy)**: Forwards traffic from the VLAN to control plane services

### Key Components
- **Infra CRD** (`api/v1alpha1/infra_types.go`): Custom resource defining infrastructure requirements
- **InfraReconciler** (`internal/controller/infra_controller.go`): Controller that provisions DHCP/DNS/Envoy services
- **API Group**: `hostedcluster.densityops.com/v1alpha1`

## Features

- 🚀 **Automatic Provisioning**: Provisions DHCP, DNS, and L4 proxy onto the VLAN from a single declarative `Infra` custom resource
- 🔒 **Network Isolation**: Supports air-gapped VLAN environments (`attach-default-network: false`) — tenant clusters never need routes into the management cluster
- 🌐 **Split-Horizon DNS**: Dual-view CoreDNS — VMs on the VLAN resolve HCP names to the external proxy, management-cluster pods resolve them to an internal proxy (or are shielded entirely)
- 📦 **Apps Ingress Automation**: Installs MetalLB in the hosted cluster, creates the wildcard `*.apps.*` LoadBalancer VIP, and wires SNI wildcard proxying plus DNS entries for both views
- 🏷️ **ExternalDNS Integration**: Declarative labels/annotations on the hosted ingress Service so public DNS records follow VIP changes automatically
- 🔌 **Static IPAM**: Fixed per-component IPs with KubeVirt-aware DHCP (detects VMI interfaces and preserves existing leases)
- 🧹 **Garbage Collection**: Owner references ensure automatic cleanup when the `Infra` resource is deleted
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

Create a HostedCluster (e.g. with `hcp create cluster kubevirt ...`), then
declare the VLAN infrastructure with an `Infra` custom resource. The operator
provisions a `DHCPServer`, `DNSServer`, and `ProxyServer` — each pinned to its
static IP on the secondary network via a Multus network-attachment — and keeps
them reconciled. Deleting the `Infra` resource garbage-collects everything.

```
KubeVirt VMs (isolated VLAN, 192.168.100.0/24)   Management cluster pod network
┌───────────────────────────┐                  ┌────────────────────────────────┐
│  DHCP ← 192.168.100.2     │                  │  Hosted control plane pods     │
│  DNS  ← 192.168.100.3 ────┼── static HCP ───▶│  kube-apiserver / oauth /      │
│  Envoy← 192.168.100.10 ───┼── SNI proxy ────▶│  ignition / konnectivity       │
│  *.apps → MetalLB VIP     │◀──── wildcard ───┤  oooi-ingress LoadBalancer     │
└───────────────────────────┘                  └────────────────────────────────┘
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
      - "resolv.conf"   # inherit the node's nameservers (or use explicit IPs)
  
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
      # Optional: pod-network view — management pods resolve HCP names to the
      # in-cluster proxy instead of public DNS. Leave unset to hide HCP from pods.
      internalProxyService: "example-infra-proxy.clusters.svc.cluster.local"
```

**Key Configuration Points**:

- **`networkConfig.dnsServers`**: Upstream DNS servers that CoreDNS forwards non-HCP queries to. Use `"resolv.conf"` on networks without direct egress to public resolvers.
- **`infraComponents.dns.enabled: true`**: When enabled, DHCP automatically uses the DNS server IP
- **`infraComponents.proxy.internalProxyService`**: Enables the pod-network split-horizon view
- **DNS Flow**: VMs → DHCP assigns CoreDNS IP → CoreDNS resolves HCP domains to the external proxy → other queries forwarded upstream

### Apps Ingress (wildcard `*.apps.*`)

With `appsIngress.enabled`, the operator installs MetalLB in the hosted
cluster, creates a wildcard LoadBalancer VIP for the default IngressController,
adds SNI wildcard backends to Envoy, and publishes `*.apps.<cluster>.<domain>`
in both DNS views:

```yaml
spec:
  appsIngress:
    enabled: true
    hostedClusterRef:
      name: "my-cluster"
      namespace: "clusters"
    metallb:
      addressPoolName: "vlan203-apps"
      ipAddressPoolRange: "192.168.100.200-192.168.100.220"
    service:
      name: "oooi-ingress"
      namespace: "openshift-ingress"
      # Merged onto the Service every reconcile; lets ExternalDNS running in
      # the hosted cluster publish and track the public wildcard record
      annotations:
        external-dns.alpha.kubernetes.io/hostname: "*.apps.my-cluster.example.com."
      labels:
        external-dns.example.com/publish: "yes"
    ports:
      http: 80
      https: 443
```

Progress is reported via `.status.appsIngressStatus` (`Pending` → `Ready` /
`Degraded`) with the assigned VIP in `.externalIP`.

See [apps-ingress.md](docs/apps-ingress.md) for public-DNS ownership patterns
and [DNS_SETUP.md](docs/DNS_SETUP.md) / [PROXY_SETUP.md](docs/PROXY_SETUP.md)
for detailed configuration.

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
- [DNS Setup](docs/DNS_SETUP.md) - Split-horizon DNS configuration, pod-network view, upstream selection, troubleshooting
- [Proxy Setup](docs/PROXY_SETUP.md) - Envoy L4 proxy configuration details
- [Apps Ingress](docs/apps-ingress.md) - Wildcard `*.apps.*` ingress, MetalLB automation, public-DNS ownership
- [Static IPAM](docs/STATIC_IPAM.md) - IP address management

## License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) for details.

## Support

- Issues: [GitHub Issues](https://github.com/cldmnky/oooi/issues)
- Discussions: [GitHub Discussions](https://github.com/cldmnky/oooi/discussions)

---

Built with ❤️ using [Kubebuilder](https://kubebuilder.io/) and [Operator SDK](https://sdk.operatorframework.io/).
