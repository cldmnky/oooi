# Configuration

Shared VLAN services are driven by the **`Infra`** custom resource, while
cluster-specific DNS, proxy, and apps-ingress settings are supplied by
**`InfraClusterAttachment`** resources. This section is the field-level
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
    cidr: 192.0.2.0/24
    gateway: 192.0.2.1
    networkAttachmentDefinition: vlan100
    networkAttachmentNamespace: default
  infraComponents:
    dhcp:
      serverIP: 192.0.2.2
      rangeStart: 192.0.2.100
      rangeEnd: 192.0.2.199
    dns:
      serverIP: 192.0.2.3
    proxy:
      serverIP: 192.0.2.4
---
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: InfraClusterAttachment
metadata:
  name: mycluster
  namespace: clusters
spec:
  infraRef:
    name: mycluster
  hostedClusterRef:
    name: mycluster
    namespace: clusters
  dns:
    clusterName: mycluster
    baseDomain: example.com
```

`dhcp.enabled`, `dns.enabled`, `proxy.enabled` default to `true`, so the above
deploys all three components.

## Configuration principles

1. **Static IPs are deliberate.** DHCP, DNS, and proxy each receive a fixed
   address inside `networkConfig.cidr` via a Multus attachment; plan them like
   you plan router interfaces.
2. **DNS names come from attachments.** `spec.dns.clusterName` +
   `spec.dns.baseDomain` construct `api.<cluster>.<domain>`,
   `*.apps.<cluster>.<domain>`, and related names.
3. **The pod-network view is opt-in.** Without `proxy.internalProxyService`,
   management pods get no static HCP answers at all.
4. **Apps ingress is a separate feature.** It requires a working hosted cluster
   and installs MetalLB *into the hosted cluster*.
