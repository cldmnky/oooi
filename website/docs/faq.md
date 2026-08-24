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

### Why not just use hosting-cluster Routes for everything?

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

On KubeVirt, no — HyperShift rejects it at admission (`spec.services` is also
immutable). Use the supported pattern instead:
[Public DNS and OAuth publishing](guides/public-dns-oauth.md).

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
