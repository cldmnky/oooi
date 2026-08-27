# oooi - OpenShift Hosted Control Plane Infrastructure Operator

[![Go Report Card](https://goreportcard.com/badge/github.com/cldmnky/oooi)](https://goreportcard.com/report/github.com/cldmnky/oooi)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

**oooi** stands for **OpenShift on OpenShift Infra**. It is a Kubernetes
operator for the DHCP, split-horizon DNS, and TLS-passthrough proxy services
needed by OpenShift Hosted Control Planes running KubeVirt workers on isolated
secondary networks.

End-user documentation: <https://cldmnky.github.io/oooi/>

## Overview

When KubeVirt workers use `attachDefaultNetwork: false`, their VLAN interface
does not have a route to HostedCluster Services on the management cluster pod
network. oooi places the required network services on that VLAN and forwards
only the configured control-plane traffic across the boundary. Envoy reads TLS
SNI but does not terminate TLS.

One `Infra` describes one shared VLAN. One or more
`InfraClusterAttachment` resources connect HostedClusters to that shared stack.
The Infra controller is the only writer of the shared `DHCPServer`,
`DNSServer`, and `ProxyServer` children.

## Getting started

The supported order is:

1. Prepare the management cluster, the secondary-network NAD, HostedCluster
   secrets, and a non-overlapping IP plan.
2. Install the oooi CRDs and controller.
3. Create the HyperShift `HostedCluster` and its KubeVirt `NodePool`.
4. Apply one `Infra` for the VLAN and one `InfraClusterAttachment` for each
   HostedCluster using it.
5. Verify the VLAN path, plus the optional management-pod and public-DNS paths
   when `internalProxyService`, public exposure, and ExternalDNS are configured.

oooi consumes HostedClusters; it does not create or delete them.

### Prerequisites

- An OpenShift Container Platform management cluster with HyperShift, Red Hat
  OpenShift Virtualization, and Multus
- `kubectl` or `oc`, and `hcp` when using the HyperShift CLI
- A Multus `NetworkAttachmentDefinition` for the isolated Layer-2 network
- HostedCluster pull, SSH, and etcd-encryption secrets
- No competing DHCP server on the VLAN
- An image registry if building oooi from source

Reserve addresses for the gateway, DHCP, DNS, proxy, worker DHCP pool, and any
MetalLB apps-ingress range. Keep those ranges disjoint.

### Build and deploy

`make container-build` uses `IMAGE_TAG_BASE` and builds a multi-architecture
image with ko. Pin the digest for reproducible deployments:

```bash
export IMAGE_TAG_BASE=registry.example.com/oooi
make container-build

export OOOI_IMAGE=registry.example.com/oooi@sha256:<digest>
make install
make deploy IMG="$OOOI_IMAGE"
kubectl -n oooi-system rollout status deployment/oooi-controller-manager --timeout=5m
```

The checked-in sample and end-user Helm defaults use
`quay.io/cldmnky/oooi:latest`. The generated component defaults use the oooi
image for DHCP and DNS, and `envoyproxy/envoy:v1.36.4` plus an oooi manager
sidecar for the proxy. Mirror and set the component image fields explicitly in
`Infra` when required by an air-gapped registry; use a digest instead of
`:latest` when deployment reproducibility is required.

### Create an `Infra` and attachment

The following values are documentation values. Replace the names, addresses,
NAD, domains, and images before applying them:

```yaml
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: Infra
metadata:
  name: tenant-vlan100
  namespace: clusters
spec:
  networkConfig:
    cidr: 192.0.2.0/24
    gateway: 192.0.2.1
    networkAttachmentDefinition: vlan100
    networkAttachmentNamespace: default
    dnsServers:
      - 198.51.100.53
  infraComponents:
    dhcp:
      serverIP: 192.0.2.2
      rangeStart: 192.0.2.100
      rangeEnd: 192.0.2.199
      leaseTime: 1h
    dns:
      serverIP: 192.0.2.3
    proxy:
      serverIP: 192.0.2.4
      internalProxyService: tenant-vlan100-proxy.clusters.svc.cluster.local
---
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: InfraClusterAttachment
metadata:
  name: example-hcp
  namespace: clusters
spec:
  infraRef:
    name: tenant-vlan100
  hostedClusterRef:
    name: example-hcp
    namespace: clusters
  dns:
    clusterName: example-hcp
    baseDomain: clusters.example.com
```

If `networkAttachmentNamespace` is empty, oooi uses the `Infra` namespace. The
attachment must be in the same namespace as its referenced `Infra`. Its
`controlPlaneNamespace` defaults to `<hostedCluster namespace>-<name>` and its
`apiServerService` defaults to `kube-apiserver`.

Apply the attachment after the HostedCluster object exists, but do not wait for
the HostedCluster to become Available. The shared services are needed while
workers bootstrap:

```bash
kubectl apply -f infra.yaml
kubectl -n clusters get infra,infraattachment
kubectl -n clusters get dhcpserver,dnsserver,proxyserver
kubectl -n clusters wait --for=condition=Ready infra/tenant-vlan100 --timeout=30m
```

Create another `InfraClusterAttachment` that references `tenant-vlan100` to
serve a second hosted cluster. Duplicate HostedCluster references and duplicate
domains exclude both conflicting attachments and set the Infra condition to a
degraded reason. They are not silently resolved.

## Routing behavior

### Fully qualified control-plane names

Each attachment contributes these names under its configured domain:

- `api.<cluster>.<baseDomain>` and `api-int.<cluster>.<baseDomain>` to the API
  Service on port `6443`
- `oauth.<cluster>.<baseDomain>` to `oauth-openshift` on port `443`
- `ignition.<cluster>.<baseDomain>` to `ignition-server-proxy` on port `443`
- `konnectivity.<cluster>.<baseDomain>` to `konnectivity-server` on port `443`

From the VLAN, the names resolve to `proxy.serverIP`. When
`internalProxyService` is configured, the pod-network DNS view resolves them
to that Service's ClusterIP instead. Other names are forwarded to
`networkConfig.dnsServers`.

### Source-scoped `kubernetes.*` aliases

For KubeVirt workers, oooi supports these aliases on the shared proxy:

```text
kubernetes
kubernetes.default
kubernetes.default.svc
kubernetes.default.svc.cluster.local
```

DNS gives every worker the same proxy address. Envoy chooses the hosted cluster
using the worker's source IP. The hostname-SNI path uses port `443`; the
in-cluster Kubernetes Service path uses port `6443` and an IP URL, so it has no
SNI. Both generated backends are source-scoped.

The Infra controller finds KubeVirt NodePools in the HostedCluster namespace,
follows their CAPI `Machine` objects in the management cluster, and reads
`Machine.status.addresses`. It keeps only addresses inside
`Infra.spec.networkConfig.cidr`, deduplicates and sorts them, then emits `/32`
`sourcePrefixRanges` on both generated alias backends.

Aliases are omitted while a KubeVirt NodePool has no usable in-CIDR Machine
address; fully qualified names remain the recommended bootstrap path. A
duplicate source IP suppresses only the conflicting alias backends and reports
`DuplicateSourceIP` on the Infra. Source matching is routing, not
authentication, so enforce anti-spoofing in the CNI or switching layer when
untrusted tenants share a VLAN.

## Apps ingress and public OAuth

Set `InfraClusterAttachment.spec.appsIngress.enabled: true` to let oooi use the
HostedCluster kubeconfig to install/configure the Red Hat MetalLB Operator,
create an `IPAddressPool` and `L2Advertisement`, and expose the default hosted
IngressController through the `openshift-ingress/oooi-ingress` LoadBalancer
Service. The attachment status records the external IP or hostname. The shared
DNS and proxy are updated only after a real endpoint exists.

The operator does not write public DNS. Put ExternalDNS where it can watch the
Service:

- Watch each HostedCluster's management-cluster `kube-apiserver` LoadBalancer
  Service for `api.<cluster>.<baseDomain>`.
- Watch the hosted cluster's `oooi-ingress` Service for
  `*.apps.<cluster>.<baseDomain>`.
- Watch the management cluster's generated
  `<attachment>-proxy-external` Service for public OAuth when the
  HostedCluster uses Route publishing.

Enable the per-cluster path on each attachment when needed:

```yaml
spec:
  externalService:
    enabled: true
    addressPoolName: hosting-public-pool
    annotations:
      external-dns.alpha.kubernetes.io/hostname: oauth.example-hcp.clusters.example.com.
    labels:
      external-dns.example.com/publish: "yes"
```

Each enabled attachment gets its own hosting-cluster LoadBalancer Service. The
Service exposes only port `443`, selects the shared Envoy pods, and is removed
when the attachment no longer enables the external Service.

## Ownership and cleanup

The three child CRs and their namespaced workloads are owned by `Infra`.
Each attachment owns its `<attachment>-proxy-external` Service. Attachment
finalizers also clean up hosted-cluster apps-ingress resources and the
cross-namespace control-plane NetworkPolicy. Those resources cannot be
garbage-collected through an `Infra` owner reference.

Delete attachments before deleting their shared Infra:

```bash
kubectl -n clusters delete infraattachment <attachment>
kubectl -n clusters delete infra <name>
make undeploy ignore-not-found=true
make uninstall ignore-not-found=true
```

Check for unowned NetworkPolicies, DHCP reader RBAC, hosted-cluster MetalLB
objects, and public DNS records after teardown.

## Development

```bash
make manifests generate
make fmt vet
make test
make lint
make build
```

Run the isolated Kind suite with Multus and the NodePool/Machine CRD stubs:

```bash
make test-e2e
```

Use `PODMAN_RUNTIME=true make test-e2e` for Podman, or set `KIND_CLUSTER` to
choose the Kind cluster name. The E2E suite is Kind-only and verifies its
current context before running.

For a production-like HyperShift/KubeVirt environment, review and run
`scripts/e2e-borg-species-8472.sh`. It exercises two attachments on one VLAN,
including source-scoped no-SNI API routing, public API/OAuth/apps DNS, and
endpoint checks. Run `scripts/cleanup-e2e-borg-species-8472.sh` afterward; it
deletes attachments before shared infrastructure and retries remaining
KubeVirt VM resources while NodePools are terminating. Override the script's
environment-specific variables before using it.

For manager flags, use `go run ./main.go manager <flags>` directly; `make run`
starts the manager without forwarding additional arguments.

## Documentation

- [End-user documentation](https://cldmnky.github.io/oooi/)
- [Architecture](website/docs/architecture.md)
- [Multiple hosted clusters](website/docs/guides/multi-cluster.md)
- [Infra reference](website/docs/configuration/infra-reference.md)
- [Split-horizon DNS](docs/DNS_SETUP.md)
- [Proxy setup](docs/PROXY_SETUP.md)
- [Apps ingress](docs/apps-ingress.md)
- [E2E setup](docs/E2E_TEST_SETUP.md)

## License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) for
details.
