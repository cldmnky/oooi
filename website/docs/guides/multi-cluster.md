# Multiple hosted clusters on one VLAN

One isolated Layer-2 network can host several HyperShift hosted clusters.
A single oooi stack serves all of them: one DHCP server, one split-horizon DNS
server, and one Envoy proxy.

The model has two resource types:

| Resource | Scope | Owns |
|---|---|---|
| `Infra` | One VLAN | CIDR, NAD, gateway, DHCP pool, DNS server IP, proxy server IP, upstream resolvers, shared external Service |
| `InfraClusterAttachment` | One hosted cluster | Cluster domain, control-plane namespace, optional apps ingress |

Each attachment contributes its DNS records and SNI backends to the shared
children. The `Infra` reconciler is the only writer of the shared
`DHCPServer`, `DNSServer`, and `ProxyServer`.

## Example

Two clusters share one VLAN with documentation-only names and addresses.
Replace every value before applying anything.

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
    dnsServers:
      - 198.51.100.53
  infraComponents:
    dhcp:
      serverIP: 192.0.2.2
      rangeStart: 192.0.2.100
      rangeEnd: 192.0.2.199
    dns:
      serverIP: 192.0.2.3
    proxy:
      serverIP: 192.0.2.4
      internalProxyService: tenant-vlan100-proxy.clusters.svc.cluster.local
---
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: InfraClusterAttachment
metadata:
  name: example-hcp-a
  namespace: clusters
spec:
  infraRef:
    name: tenant-vlan100
  hostedClusterRef:
    name: example-hcp-a
  dns:
    clusterName: example-hcp-a
    baseDomain: clusters.example.com
---
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: InfraClusterAttachment
metadata:
  name: example-hcp-b
  namespace: clusters
spec:
  infraRef:
    name: tenant-vlan100
  hostedClusterRef:
    name: example-hcp-b
  dns:
    clusterName: example-hcp-b
    baseDomain: clusters.example.com
```

The generated records per attachment:

- `api`, `api-int`, `oauth`, `ignition`, and `konnectivity` under
  `<clusterName>.<baseDomain>` resolve to the shared proxy on the VLAN, and to
  the shared proxy ClusterIP from management pods.
- Envoy routes each fully qualified SNI name to that cluster's control-plane
  namespace, which defaults to `<hostedClusterRef.namespace>-<hostedClusterRef.name>`.

## Apps ingress per cluster

Enable `spec.appsIngress` on each attachment independently. Every attachment
gets its own MetalLB address pool, and the ranges must be disjoint:

```yaml
spec:
  appsIngress:
    enabled: true
    metallb:
      addressPoolName: example-hcp-a-apps
      ipAddressPoolRange: 192.0.2.200-192.0.2.209
```

Each attachment's wildcard VIP is reported on its own status, so one cluster's
ingress can be Pending while another is Ready.

## Public OAuth through the shared proxy VIP

When the shared proxy external Service is enabled on the `Infra`, set
`publishAttachmentOAuths: true` to append each Ready attachment's
`oauth.<clusterName>.<baseDomain>` name to the ExternalDNS hostname
annotation as a comma-separated list:

```yaml
infraComponents:
  proxy:
    externalService:
      enabled: true
      publishAttachmentOAuths: true
      addressPoolName: hosting-public-pool
      annotations:
        external-dns.alpha.kubernetes.io/hostname: "shared-endpoint.example.com."
```

Names already configured keep their position; attachment names follow,
sorted. Verify your ExternalDNS provider supports multiple hostnames on a
single Service annotation before relying on this.

## Failure modes

| Symptom | Reason | Resolution |
|---|---|---|
| `Infra Ready=False`, reason `DuplicateHostname` | Two attachments declare the same domain | Remove or rename one attachment's `dns` values |
| `Infra Ready=False`, reason `DuplicateHostedCluster` | Two attachments reference the same HostedCluster | Keep one attachment per HostedCluster |
| Attachment stuck without `Ready` | Its control-plane namespace does not exist yet | Create the HostedCluster first; HyperShift creates `<ns>-<name>` |
| ProxyServer has no routes after edits | A conflict excluded every attachment; stale shared routing is removed | Fix the conflict listed in the Infra condition message |

## Fully qualified SNI names

Every attachment contributes fully qualified SNI names such as
`api.example-hcp-a.clusters.example.com`. Unqualified Kubernetes aliases are
not generated because a shared proxy cannot safely route them to one cluster.

There is no implicit single-cluster binding. To serve a cluster, create an
`InfraClusterAttachment`, even when the `Infra` has only one attachment.
