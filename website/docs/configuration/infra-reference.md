# Infra CR reference

Complete reference for `Infra` (`hostedcluster.densityops.com/v1alpha1`,
namespaced, short name `infra`). Fields marked **Required** are enforced by the
API schema or runtime validation; everything else is optional with the listed
default.

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
| `baseDomain` | string | *(empty)* | Base domain of the hosted cluster, e.g. `clusters.example.com`. |
| `clusterName` | string | *(empty)* | Hosted cluster name; combined with `baseDomain` builds FQDNs (`api.<clusterName>.<baseDomain>`). |
| `image` | string | oooi image | Override the image that runs the CoreDNS component. |

Static answers generated per view:

| Name(s) | VLAN view answers | Pod-network view answers |
|---|---|---|
| `api.<cluster>.<domain>` | `proxy.serverIP` | `proxy.internalProxyService` ClusterIP |
| `api-int.<cluster>.<domain>` | `proxy.serverIP` | `internalProxyService` |
| `oauth.*`, `ignition.*`, `konnectivity.*` | `proxy.serverIP` | `internalProxyService` |
| `*.apps.<cluster>.<domain>` (when apps ingress is Ready) | MetalLB external IP | proxy ClusterIP |

All other names are forwarded to `networkConfig.dnsServers`.

## spec.infraComponents.proxy

Envoy L4 SNI-passthrough gateway. `enabled` defaults to `true`.

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `true` | Deploy the ProxyServer child and workload. |
| `serverIP` | string | *(empty)* | Static VLAN address of Envoy. |
| `controlPlaneNamespace` | string | *(empty)* | Management-cluster namespace hosting the control-plane Services (`clusters-<name>`). |
| `apiServerService` | string | `kube-apiserver` | Reserved API field. `Infra` currently always routes API traffic to the `kube-apiserver` Service. |
| `internalProxyService` | string | *(empty)* | DNS name **or** ClusterIP of the proxy Service used in the pod-network DNS view. Omit to hide HCP names from pods. Example: `<infra>-proxy.clusters.svc.cluster.local`. |
| `proxyImage` | string | `envoyproxy/envoy:v1.36.4` | Envoy image. |
| `managerImage` | string | oooi image | xDS manager sidecar image. |

Port model: `6443` → API service; all other configured backends → `443`; Envoy
admin `9901` remains internal (ClusterIP only).

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

Use this to put OAuth and other Route-published endpoints on a public VIP. See
[Public DNS and OAuth publishing](../guides/public-dns-oauth.md).

## spec.appsIngress

Optional wildcard `*.apps` automation. All sub-fields optional; `enabled`
defaults to `false`.

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Enable apps-ingress automation. |
| `baseDomain` | string | `<dns.clusterName>.<dns.baseDomain>` | Overrides the domain used for the wildcard. When set, oooi creates `*.apps.<baseDomain>`. |
| `hostedClusterRef.name` | string | *(empty)* | HostedCluster whose ingress/MetalLB will be configured. Required when enabled. |
| `hostedClusterRef.namespace` | string | `clusters` | Namespace of that HostedCluster. |
| `metallb.addressPoolName` | string | *(empty)* | IPAddressPool created in the hosted cluster (`openshift-operators`). |
| `metallb.ipAddressPoolRange` | string | *(empty)* | Required when apps ingress is enabled. Passed to the hosted-cluster `IPAddressPool` as `spec.addresses`; use a valid MetalLB range or CIDR that is unused, routable L2 space reachable from workers. |
| `metallb.l2AdvertisementName` | string | `advertise-<addressPoolName>` | L2Advertisement resource name. |
| `service.name` | string | `oooi-ingress` | LoadBalancer Service name created in the hosted cluster. |
| `service.namespace` | string | `openshift-ingress` | Namespace for that Service. |
| `service.labels` / `service.annotations` | map[string]string | *(empty)* | Merged into the Service on every reconcile; unrelated existing metadata preserved. The `metallb.universe.tf/address-pool` annotation is operator-owned — do not set it here. Intended for ExternalDNS hostname/publish metadata. |
| `ports.http` / `ports.https` | int32 | `80` / `443` | Ports of the LoadBalancer Service and matching Envoy backends. |

Behavior sequence and status phases are described in
[Apps ingress and MetalLB](../guides/apps-ingress.md).

## status

| Field | Description |
|---|---|
| `conditions[]` | Reconciliation status. `Ready=True` confirms that oooi provisioned the declared components; it does not confirm Deployment availability. |
| `componentStatus.dhcpReady` / `.dnsReady` / `.proxyReady` | Provisioning booleans set when the corresponding component is enabled. Check Deployment status for runtime readiness. |
| `appsIngressStatus.phase` | `Pending`, `Ready`, or `Degraded`. |
| `appsIngressStatus.reason` | Machine-readable phase reason, e.g. `WaitingForHostedClusterNodes`, `WaitingForMetalLBCRDs`, `WaitingForExternalIP`, `ReconciliationSucceeded`, `HostedClusterAccessFailed`, `MetalLBInstallFailed`, `IngressServiceFailed`, `ExternalIPDiscoveryFailed`. |
| `appsIngressStatus.message` | Human-readable detail when Degraded. |
| `appsIngressStatus.externalIP` | VIP assigned by MetalLB (when reported as an IP). |
| `appsIngressStatus.externalHostname` | Assigned hostname (cloud-style LBs). |
| `observedGeneration` | Generation last reconciled. |

## Immutability notes

- `spec.services` on the *HostedCluster* (not Infra) is immutable after
  creation — plan publishing strategies up front.
- Infra fields are mutable. Setting a component's `enabled` field to `false`
  stops its reconciliation but does not delete an existing child resource;
  delete that child explicitly when retiring the component.
