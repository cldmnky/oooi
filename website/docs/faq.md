# FAQ

## General

### What does "oooi" stand for?

**OpenShift on OpenShift Infra**. It provides the DHCP, DNS, and proxy services
needed when one OpenShift cluster hosts another on an isolated VLAN.

### Does oooi create hosted clusters?

No. oooi consumes an existing `HostedCluster`; it provisions the *network
infrastructure* the cluster needs. Create the HostedCluster with `hcp` or your
own manifests, then apply `Infra`.

### Does oooi terminate TLS?

Never. The Envoy proxy routes by SNI and forwards encrypted bytes. Certificates
stay inside the hosted cluster.

### Why not use hosting-cluster Routes for everything?

You can — but tenant traffic then traverses hosting-cluster ingress, breaking
the isolation that motivated isolated VLANs in the first place. oooi keeps the
only cross-network paths narrow: specific SNI names to specific Services.

## Networking

### Can I run my own DHCP server alongside oooi?

Not on the same L2. Competing DHCP authorities cause non-deterministic worker
bootstrapping. oooi must be the only DHCP server on the VLAN.

### What if I omit internalProxyService?

The pod-network DNS view publishes no static HCP answers. Management pods can't
resolve hosted endpoints by name — a valid hardening choice.

### Which MetalLB modes are supported for apps ingress?

L2 advertisement (default configuration created by oooi). BGP and dual-stack
are untested with this flow; validate separately before relying on them.

### Can OAuth use LoadBalancer publishing directly?

For the KubeVirt configuration documented here, use the `Route` publishing
strategy and expose OAuth through the proxy external Service when public access
is required. `spec.services` is immutable after HostedCluster creation. See:
[Public DNS and OAuth publishing](guides/public-dns-oauth.md).

### Can multiple hosted clusters share one VLAN?

Yes. Create one `Infra` for the network and one
`InfraClusterAttachment` per hosted cluster; oooi aggregates every
attachment's DNS records and SNI backends onto a single DHCP/DNS/proxy stack.
See [Multiple hosted clusters on one VLAN](guides/multi-cluster.md).

### Why does the proxy not answer kubernetes.default.svc?

Unqualified Kubernetes service names are ambiguous when several clusters share
a proxy: they cannot be routed to one cluster safely. oooi generates only fully
qualified names such as `api.<cluster>.<domain>`, which worker bootstrap uses.

## Operations

### How do I know everything is healthy?

```bash
kubectl -n clusters get infra <name>
```

plus the three-path verification in [Verification](operations/verify.md).

### What happens when I delete Infra?

Everything it owns is garbage-collected: child CRs, Deployments, Services,
ConfigMaps, ServiceAccounts, SCC RoleBindings, and the namespace NetworkPolicy.
The HostedCluster itself is untouched until you delete it separately.

### Where do public DNS records come from?

From *your* ExternalDNS instances watching the appropriate Services. oooi only
attaches the labels/annotations those integrations need. See the
[ownership matrix](guides/public-dns-oauth.md#ownership-matrix).

### Is there a validating webhook?

Not currently; invalid configurations surface as `Degraded` conditions with a
message rather than being rejected at admission.
