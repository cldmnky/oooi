# Configuration

Everything oooi does is driven by the **`Infra`** custom resource
(`hostedcluster.densityops.com/v1alpha1`). This section is the field-level
reference; for copy-paste manifests see [Examples](../examples/index.md).

<div class="grid cards" markdown>

-   :material-book-open-outline: __Infra CR reference__

    ---

    Every spec and status field, with defaults and constraints.

    [Open the reference :material-arrow-right:](infra-reference.md)

</div>

## Minimal viable configuration

```yaml
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: Infra
metadata:
  name: mycluster
  namespace: clusters
spec:
  networkConfig:
    cidr: 192.168.100.0/24
    gateway: 192.168.100.1
    networkAttachmentDefinition: vlan100
  infraComponents:
    dhcp:
      serverIP: 192.168.100.2
      rangeStart: 192.168.100.100
      rangeEnd: 192.168.100.200
    dns:
      serverIP: 192.168.100.3
      clusterName: mycluster
      baseDomain: example.com
    proxy:
      serverIP: 192.168.100.10
      controlPlaneNamespace: clusters-mycluster
```

`dhcp.enabled`, `dns.enabled`, `proxy.enabled` default to `true`, so the above
deploys all three components.

## Configuration principles

1. **Static IPs are deliberate.** DHCP, DNS, and proxy each receive a fixed
   address inside `networkConfig.cidr` via a Multus attachment; plan them like
   you plan router interfaces.
2. **DNS names derive from two fields.** `dns.clusterName` + `dns.baseDomain`
   construct `api.<cluster>.<domain>`, `*.apps.<cluster>.<domain>`, etc.
3. **The pod-network view is opt-in.** Without `proxy.internalProxyService`,
   management pods get no static HCP answers at all.
4. **Apps ingress is a separate feature.** It requires a working hosted cluster
   and installs MetalLB *into the hosted cluster*.
