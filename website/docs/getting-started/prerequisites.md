# Prerequisites

Everything a sysadmin must have in place before deploying oooi. Work through
this page top to bottom; each section includes a check you can run.

## Management cluster

oooi runs on an **OpenShift management cluster** (the *hosting* cluster) that
provides the following capabilities:

| Capability | Why it is needed | Verify |
|---|---|---|
| HyperShift operator (hosted control planes) | Creates and runs hosted control planes; provides the `HostedCluster`/`NodePool` APIs | `oc get pods -n hypershift` |
| OpenShift Virtualization | Runs the worker VMs as KubeVirt `VirtualMachine`s | `oc get csv -n openshift-cnv` |
| Multus (built-in) | Attaches oooi pods and worker VMs to the secondary VLAN via `NetworkAttachmentDefinition` | `oc auth can-i get net-attach-def` |
| MetalLB operator access | Allocates LoadBalancer VIPs for apps ingress and the external proxy Service | `oc auth can-i create ipaddresspool.metallb.io` |
| OLM | Installs MetalLB into the hosted cluster during apps-ingress automation | `oc get csv -n operators` |

A management cluster with at least one worker node is sufficient for lab use.
Size production clusters per your HCP density targets.

## Secondary network (VLAN)

The worker VMs must be attached to an isolated Layer-2 network represented by a
Multus `NetworkAttachmentDefinition`. In this design the VMs have **no default
cluster network** (`attachDefaultNetwork: false`), so this VLAN is their only
connectivity.

Requirements:

- **DHCP disabled upstream.** oooi must be the *only* DHCP server on the VLAN.
  Competing DHCP services cause non-deterministic worker bootstrapping.
- **Routable, unused IP space.** You need addresses for three static services
  (DHCP, DNS, proxy), one DHCP pool for workers, and — if enabling apps
  ingress — a MetalLB range on the same L2 segment.
- **Upstream DNS reachable.** The CoreDNS forwarders (`networkConfig.dnsServers`)
  must be reachable from the VLAN or from the pod network.

Create (or verify) the NAD, e.g. `default/vlan203`:

```yaml
apiVersion: k8s.cni.cncf.io/v1
kind: NetworkAttachmentDefinition
metadata:
  name: vlan203
  namespace: default
spec:
  config: |-
    {
      "cniVersion": "0.3.1",
      "name": "vlan203",
      "type": "bridge",
      "bridge": "br-vlan203",
      "vlan": 203,
      "ipam": {}
    }
```

```bash
kubectl get net-attach-def -n default vlan203
```

!!! note "Bridge name"

    The bridge/CNI details are node-environment specific (Linux bridge, OVS,
    SR-IOV). What oooi requires is simply a working `NetworkAttachmentDefinition`
    that puts pods/VMs on the target L2 segment with no conflicting IPAM.

## Hosted cluster namespace secrets

HyperShift expects these secrets in the `HostedCluster` namespace (typically
`clusters`). oooi does not create them, but the hosted cluster cannot bootstrap
without them:

```bash
kubectl get secret -n clusters \
  pullsecret-clusters \
  sshkey-clusters \
  etcd-encryption-key-clusters
```

| Secret | Purpose |
|---|---|
| `pullsecret-clusters` | Registry pull secret for the release payload |
| `sshkey-clusters` | SSH key injected into workers |
| `etcd-encryption-key-clusters` | etcd encryption key for the hosted cluster |

## Tools

| Tool | Required for | Notes |
|---|---|---|
| `oc` / `kubectl` | Everything | Match your OpenShift version |
| `hcp` CLI | Creating KubeVirt hosted clusters | From the HyperShift CLI |
| Go 1.24+ | Building oooi images from source | Not needed if using published images |
| `ko` (via Makefile) | `make container-build` | Auto-downloaded into `./bin` |
| `dig`, `curl`, `openssl` | Verification from a VLAN client | Any Linux box on the VLAN works |

## IP planning worksheet

Reserve addresses before writing the `Infra` resource. Example plan for a
`10.202.64.0/24` VLAN (gateway `.1`) modeled on the validated sample:

| Assignment | Address | Notes |
|---|---|---|
| VLAN gateway | `10.202.64.1` | Provided by your router/switch |
| DHCPServer static IP | `10.202.64.2` | Must be inside the CIDR |
| DNSServer static IP | `10.202.64.3` | Must be inside the CIDR |
| ProxyServer static IP | `10.202.64.4` | Must be inside the CIDR |
| DHCP pool (worker VMs) | `10.202.64.200–10.202.64.254` | Exclude all statics above |
| Apps ingress MetalLB range | `10.202.64.180–10.202.64.190` | L2-advertised VIPs; keep clear of gateway/statics/pool |
| External proxy Service (optional) | auto from hosting-cluster MetalLB pool | e.g. `metallb-pool`; used to publish OAuth publicly |

!!! warning "Keep ranges disjoint"

    The MetalLB `ipAddressPoolRange` must be an **unused, routable range on the
    same L2 network** as the hosted worker interfaces. Do not include the
    gateway, the DHCP/DNS/proxy static IPs, the DHCP pool, or another load
    balancer's addresses.

## Upstream resolvers decision

Decide how non-cluster DNS queries leave the environment:

- Explicit resolvers (recommended): list IPs in `networkConfig.dnsServers`.
- `"resolv.conf"`: inherit the node's nameservers — useful when there is no
  direct egress to public resolvers.

See [Split-horizon DNS](../guides/split-horizon-dns.md) for how the two views
use these resolvers.
