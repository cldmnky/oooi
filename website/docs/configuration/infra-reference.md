# Infra CR reference

Complete reference for `Infra` (`hostedcluster.densityops.com/v1alpha1`,
namespaced, short name `infra`). Fields marked **Required** are enforced by the
API schema or runtime validation; everything else is optional with the listed
default.

An `Infra` contains only shared VLAN and component configuration. Create an
`InfraClusterAttachment` for every hosted cluster that should use the shared
stack; without an attachment, no cluster-specific DNS or proxy routes are
generated.

## spec.networkConfig

Defines the secondary network. **Required.**

| Field | Type | Default | Description |
|---|---|---|---|
| `cidr` | string | — (**Required**) | IP range of the secondary network, CIDR notation, e.g. `192.0.2.0/24`. Validated pattern. |
| `gateway` | string | — (**Required**) | Default gateway on that network, e.g. `192.0.2.1`. |
| `networkAttachmentDefinition` | string | — (**Required**) | Name of the Multus NAD representing the VLAN. |
| `networkAttachmentNamespace` | string | *(empty)* | Namespace of the NAD. When empty, oooi uses the namespace of the `Infra` resource. |
| `dnsServers` | []string | *(empty)* | Upstream DNS servers used by CoreDNS forwarding. Entries may be IPs or the literal `"resolv.conf"` to inherit node resolvers. |

## spec.infraComponents.dhcp

DHCP for worker VMs. `enabled` defaults to `true`.

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `true` | Deploy the DHCPServer child and workload. |
| `serverIP` | string | *(empty)* | Static VLAN address of the DHCP server. Must be inside `networkConfig.cidr`. |
| `rangeStart` / `rangeEnd` | string | *(empty)* | Inclusive dynamic pool handed to VMs. Exclude all static addresses and MetalLB ranges. |
| `leaseTime` | duration | `1h` | Lease duration (Go duration syntax: `30m`, `1h`, `24h`). |
| `image` | string | built-in | Override the DHCP server image (e.g. mirror registry). |

The server is KubeVirt-aware: it inspects `VirtualMachineInstance` interfaces
so re-reconciliation does not churn existing leases.

## spec.infraComponents.dns

Split-horizon CoreDNS. `enabled` defaults to `true`.

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `true` | Deploy the DNSServer child and workload. |
| `serverIP` | string | *(empty)* | Static VLAN address of CoreDNS. |
| `image` | string | oooi image | Override the image that runs the CoreDNS component. |

Static answers generated per view:

The pod-network column applies only when `internalProxyService` is configured.
Without it, static HCP answers, including apps and alias names, are omitted
from the default view and queries are forwarded upstream.

| Name(s) | VLAN view answers | Pod-network view answers |
|---|---|---|
| `api.<cluster>.<domain>` | `proxy.serverIP` | `proxy.internalProxyService` ClusterIP |
| `api-int.<cluster>.<domain>` | `proxy.serverIP` | `internalProxyService` ClusterIP |
| `oauth.*`, `ignition.*`, `konnectivity.*` | `proxy.serverIP` | `internalProxyService` ClusterIP |
| `*.apps.<cluster>.<domain>` (when apps ingress is Ready) | MetalLB external IP | proxy ClusterIP |
| `kubernetes`, `kubernetes.default`, `kubernetes.default.svc`, `kubernetes.default.svc.cluster.local` (when a KubeVirt worker source range is known) | `proxy.serverIP` | `proxy.internalProxyService` ClusterIP |

All other names are forwarded to `networkConfig.dnsServers`.

## spec.infraComponents.proxy

Envoy L4 SNI-passthrough gateway. `enabled` defaults to `true`.

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `true` | Deploy the ProxyServer child and workload. |
| `serverIP` | string | *(empty)* | Static VLAN address of Envoy. |
| `internalProxyService` | string | *(empty)* | DNS name **or** ClusterIP of the proxy Service used in the pod-network DNS view. Omit to hide HCP names from pods. Example: `<infra>-proxy.clusters.svc.cluster.local`. |
| `proxyImage` | string | `envoyproxy/envoy:v1.36.4` | Envoy image. |
| `managerImage` | string | oooi image | xDS manager sidecar image. |

Generated control-plane backends use `6443` for API traffic and `443` for OAuth,
Ignition, konnectivity, and Kubernetes aliases. Apps-ingress backends use the
configured HTTP/HTTPS ports. Envoy admin `9901` remains internal (ClusterIP
only).

### spec.infraComponents.proxy.externalService

Optional second Service exposing the proxy through a hosting-cluster
LoadBalancer. Created as `<infra>-proxy-external`, type `LoadBalancer`,
selecting the same Envoy pod, exposing **only** the configured ingress port.

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Create/reconcile the external LoadBalancer Service. Setting false removes an existing one. |
| `addressPoolName` | string | *(empty)* | Set as annotation `metallb.universe.tf/address-pool` to pin a MetalLB pool. Empty = cluster default allocation. |
| `labels` | map[string]string | *(empty)* | Reconciled onto the Service metadata. Use e.g. your ExternalDNS publish label. |
| `annotations` | map[string]string | *(empty)* | Reconciled onto the Service metadata. Use e.g. `external-dns.alpha.kubernetes.io/hostname: oauth.<cluster>.<domain>.` |
| `publishAttachmentOAuths` | bool | `false` | Append each Ready attachment's `oauth.<domain>` name to the hostname annotation as a comma-separated list. See [Multiple hosted clusters on one VLAN](../guides/multi-cluster.md). |

Use this to put OAuth and other Route-published endpoints on a public VIP. See
[Public DNS and OAuth publishing](../guides/public-dns-oauth.md).

## status

| Field | Description |
|---|---|
| `conditions[]` | Reconciliation status. `Ready=True` confirms that oooi provisioned the declared components; it does not confirm Deployment availability. |
| `componentStatus.dhcpReady` / `.dnsReady` | Provisioning booleans set when the corresponding component is enabled. Check Deployment status for runtime readiness. |
| `componentStatus.proxyReady` | True when an enabled proxy has at least one valid aggregated backend; it does not indicate Deployment availability. |
| `attachments.total` / `.ready` | Count of `InfraClusterAttachment` resources targeting this Infra, and how many are Ready. |
| `observedGeneration` | Generation last reconciled. |

## Immutability notes

- `spec.services` on the *HostedCluster* (not Infra) is immutable after
  creation — plan publishing strategies up front.
- Infra fields are mutable. Setting a component's `enabled` field to `false`
  stops its reconciliation but does not delete an existing child resource;
  delete that child explicitly when retiring the component.

## InfraClusterAttachment reference

`InfraClusterAttachment` (`hostedcluster.densityops.com/v1alpha1`, short names
`infraattachment`, `ica`) attaches one HostedCluster to a shared `Infra`.
Create it in the same namespace as the referenced `Infra`.

### spec

| Field | Type | Default | Description |
|---|---|---|---|
| `infraRef.name` | string | — (**Required**) | Shared `Infra` in the same namespace. |
| `hostedClusterRef.name` / `.namespace` | string | namespace: `clusters` (**Required**) | HyperShift HostedCluster to attach. One attachment per HostedCluster. |
| `dns.clusterName` / `dns.baseDomain` | string | — (**Required**) | Build the cluster's FQDNs on the shared DNS and proxy. |
| `controlPlaneNamespace` | string | `<hc-namespace>-<hc-name>` | Management-cluster namespace hosting this control plane. Must exist; HyperShift creates it. |
| `apiServerService` | string | `kube-apiserver` | API Service used when the attachment controller builds a hosted-cluster client for apps ingress. Shared proxy API backends currently target `kube-apiserver`. |
| `appsIngress` | object | *(empty)* | Per-cluster apps ingress. When enabled, MetalLB ranges must be disjoint across attachments sharing a VLAN. |

### status

| Field | Description |
|---|---|
| `conditions[]` | `Ready` reflects aggregation plus apps-ingress state. |
| `domain` | Resolved `<clusterName>.<baseDomain>`. |
| `controlPlaneNamespace` | Resolved control-plane namespace. |
| `appsIngressStatus.*` | Apps-ingress phase, endpoint, last-sync, and applied-resource identity fields scoped to this cluster; applied identities let cleanup remove resources after a configuration change or disable. |

### Source-scoped Kubernetes aliases

For KubeVirt NodePools, the controller watches CAPI `Machine` objects in the
management cluster. CAPK mirrors VMI interface addresses into
`Machine.status.addresses`; oooi retains only addresses inside
`spec.networkConfig.cidr`, deduplicates and sorts them, and emits `/32`
`sourcePrefixRanges` on the generated alias backend. NodePool membership is
used to associate Machines with the attachment, while Machine updates drive
address changes.

The aliases are omitted while a KubeVirt NodePool has no usable in-CIDR Machine
address. A duplicate source address claimed by two attachments removes only the
ambiguous alias routes and sets the Infra condition to `DuplicateSourceIP`; the
fully qualified attachment routes remain available. Source matching is not
anti-spoofing or tenant authentication.

Conflicts are visible rather than resolved silently: duplicate domains or
duplicate HostedCluster references exclude both attachments from routing and
set the referenced Infra's `Ready` condition to `False`. See
[Multiple hosted clusters on one VLAN](../guides/multi-cluster.md).
