# Examples

Ready-to-adapt manifests. Replace names, domains, CIDRs, and image references
with your environment's values.

<div class="grid cards" markdown>

-   :seedling: __Minimal VLAN__

    ---

    Smallest useful `Infra`: DHCP + DNS + proxy, no apps ingress.

    [View](#minimal-vlan-infrastructure)

-   :octicons-stack-24: __Full hosted cluster stack__

    ---

    A complete, generic configuration: apps ingress, MetalLB, ExternalDNS
    metadata, and the pod-network proxy view.

    [View](#full-hosted-cluster-stack)

-   :material-shield-lock-outline: __Public OAuth VIP__

    ---

    Publishing the OAuth endpoint through the hosting-cluster MetalLB.

    [View](#public-oauth-vip)

-   :material-hub-outline: __Multi-cluster VLAN__

    ---

    One shared Infra serving several hosted clusters via attachments.

    [Read the guide](../guides/multi-cluster.md)

</div>

## Minimal VLAN infrastructure

One isolated network, three services, nothing else:

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
    dnsServers:
      - "resolv.conf"          # inherit node resolvers
  infraComponents:
    dhcp:
      serverIP: 192.0.2.2
      rangeStart: 192.0.2.100
      rangeEnd: 192.0.2.199
    dns:
      serverIP: 192.0.2.3
    proxy:
      serverIP: 192.0.2.10
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

What you get:

| Resource | Address | Purpose |
|---|---|---|
| `DHCPServer/mycluster-dhcp` | `192.0.2.2` | Leases `.100–.199` to worker VMs |
| `DNSServer/mycluster-dns` | `192.0.2.3` | Split-horizon answers + forwarding |
| `ProxyServer/mycluster-proxy` | `192.0.2.10` | SNI passthrough for `api` :6443 and HCP :443 |

No `internalProxyService` → management pods get no static HCP answers.
No attachment `appsIngress` → no wildcard routing; consoles reachable only from
the VLAN via explicit hostnames you add yourself.

## Full hosted cluster stack

This configuration shows a complete HostedCluster/NodePool attachment with
documentation-only names and addresses:

```yaml
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: Infra
metadata:
  name: example-hcp
  namespace: clusters
spec:
  networkConfig:
    cidr: 192.0.2.0/24
    gateway: 192.0.2.1
    networkAttachmentDefinition: vlan100
    networkAttachmentNamespace: default
    dnsServers:
      - 198.51.100.53
      - 198.51.100.54
  infraComponents:
    dhcp:
      enabled: true
      serverIP: 192.0.2.2
      rangeStart: 192.0.2.100
      rangeEnd: 192.0.2.199
      leaseTime: 1h
    dns:
      enabled: true
      serverIP: 192.0.2.3
    proxy:
      enabled: true
      serverIP: 192.0.2.4
      internalProxyService: example-hcp-proxy.clusters.svc.cluster.local
      externalService:
        enabled: true
        addressPoolName: hosting-public-pool
        annotations:
          external-dns.alpha.kubernetes.io/hostname: oauth.example-hcp.clusters.example.com.
        labels:
          external-dns.example.com/publish: "yes"
---
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: InfraClusterAttachment
metadata:
  name: example-hcp
  namespace: clusters
spec:
  infraRef:
    name: example-hcp
  hostedClusterRef:
    name: example-hcp
    namespace: clusters
  dns:
    clusterName: example-hcp
    baseDomain: clusters.example.com
  appsIngress:
    enabled: true
    metallb:
      addressPoolName: apps-pool
      ipAddressPoolRange: 192.0.2.200-192.0.2.220
    service:
      name: oooi-ingress
      namespace: openshift-ingress
      annotations:
        external-dns.alpha.kubernetes.io/hostname: "*.apps.example-hcp.clusters.example.com."
      labels:
        external-dns.example.com/publish: "yes"
    ports:
      http: 80
      https: 443
```

Resulting addressing on the VLAN:

```text
192.0.2.1     gateway
192.0.2.2     DHCP
192.0.2.3     CoreDNS
192.0.2.4     Envoy (SNI proxy)
192.0.2.200   *.apps wildcard VIP (MetalLB L2, advertised by a worker)
192.0.2.100+  DHCP pool for worker VMs
```

Plus one hosting-cluster MetalLB VIP for `<infra>-proxy-external` (OAuth).

When the KubeVirt worker Machines report addresses in `192.0.2.0/24`, the
shared proxy also receives one source-scoped backend for the four
`kubernetes.*` aliases. Inspect the generated ranges with:

```bash
kubectl -n clusters get proxyserver example-hcp-proxy \
  -o jsonpath='{range .spec.backends[?(@.name=="example-hcp-kubernetes-hostname")].sourcePrefixRanges}{.}{"\n"}{end}'
```

The aliases are omitted until those Machine addresses are available. Fully
qualified `api.example-hcp.clusters.example.com` remains the bootstrap path.

## Public OAuth VIP

The minimal delta that puts OAuth on a public VIP when the HostedCluster uses
the `Route` OAuth publishing strategy (see
[Public DNS and OAuth publishing](../guides/public-dns-oauth.md)):

```yaml
infraComponents:
  proxy:
    externalService:
      enabled: true
      addressPoolName: hosting-public-pool
      annotations:
        external-dns.alpha.kubernetes.io/hostname: oauth.mycluster.example.com.
      labels:
        external-dns.example.com/publish: "yes"
```

Behavior notes:

- Creates Service `mycluster-proxy-external`, type `LoadBalancer`, port 443
  only — never Envoy admin `9901`.
- `addressPoolName` becomes the `metallb.universe.tf/address-pool` annotation;
  omit it to use the cluster's default allocation policy.
- Labels/annotations are reconciled on every sync; remove `enabled` (or set it
  false) and the Service is deleted — then ExternalDNS cleans up its records.

## More samples in the repository

| File | Shows |
|---|---|
| `config/samples/hostedcluster_v1alpha1_infra.yaml` | Infra scaffold |
| `config/samples/hostedcluster_v1alpha1_infraclusterattachment.yaml` | Attachment scaffold |
| `config/samples/hostedcluster_v1alpha1_dhcpserver.yaml` etc. | Individual child CRs |
| Hosted-cluster ExternalDNS sample | The repository includes a lab-specific manifest; adapt its structure, not its identifiers |
| `config/samples/openshift-example.yaml` | OpenShift-flavored example |
