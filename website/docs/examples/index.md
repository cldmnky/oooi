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

    The validated lab configuration: apps ingress, MetalLB, ExternalDNS
    metadata, and pod-network proxy view.

    [View](#full-hosted-cluster-stack)

-   :material-shield-lock-outline: __Public OAuth VIP__

    ---

    Publishing the OAuth endpoint through the hosting-cluster MetalLB.

    [View](#public-oauth-vip)

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
    cidr: 192.168.100.0/24
    gateway: 192.168.100.1
    networkAttachmentDefinition: vlan100
    dnsServers:
      - "resolv.conf"          # inherit node resolvers
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

What you get:

| Resource | Address | Purpose |
|---|---|---|
| `DHCPServer/mycluster-dhcp` | `192.168.100.2` | Leases `.100–.200` to worker VMs |
| `DNSServer/mycluster-dns` | `192.168.100.3` | Split-horizon answers + forwarding |
| `ProxyServer/mycluster-proxy` | `192.168.100.10` | SNI passthrough for `api` :6443 and HCP :443 |

No `internalProxyService` → management pods get no static HCP answers.
No `appsIngress` → no wildcard routing; consoles reachable only from the VLAN
via explicit hostnames you add yourself.

## Full hosted cluster stack

Modeled on the validated sample
([`config/samples/species-8472-infra.yaml`](https://github.com/cldmnky/oooi/blob/main/config/samples/species-8472-infra.yaml)):

```yaml
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: Infra
metadata:
  name: species-8472
  namespace: clusters
spec:
  networkConfig:
    cidr: 10.202.64.0/24
    gateway: 10.202.64.1
    networkAttachmentDefinition: vlan203
    networkAttachmentNamespace: default
    dnsServers:
      - 10.201.0.2
      - 10.201.0.1
  infraComponents:
    dhcp:
      enabled: true
      serverIP: 10.202.64.2
      rangeStart: 10.202.64.200
      rangeEnd: 10.202.64.254
      leaseTime: 1h
    dns:
      enabled: true
      serverIP: 10.202.64.3
      clusterName: species-8472
      baseDomain: clusters.example.com
    proxy:
      enabled: true
      serverIP: 10.202.64.4
      controlPlaneNamespace: clusters-species-8472
      apiServerService: kube-apiserver
      internalProxyService: species-8472-proxy.clusters.svc.cluster.local
      externalService:
        enabled: true
        addressPoolName: metallb-pool
        annotations:
          external-dns.alpha.kubernetes.io/hostname: oauth.species-8472.clusters.example.com.
        labels:
          external-dns.blahonga.me/publish: "yes"
  appsIngress:
    enabled: true
    hostedClusterRef:
      name: species-8472
      namespace: clusters
    metallb:
      addressPoolName: vlan203-apps
      ipAddressPoolRange: 10.202.64.180-10.202.64.190
    service:
      name: oooi-ingress
      namespace: openshift-ingress
      annotations:
        external-dns.alpha.kubernetes.io/hostname: "*.apps.species-8472.clusters.example.com."
      labels:
        external-dns.blahonga.me/publish: "yes"
    ports:
      http: 80
      https: 443
```

Resulting addressing on the VLAN:

```text
10.202.64.1     gateway
10.202.64.2     DHCP
10.202.64.3     CoreDNS
10.202.64.4     Envoy (SNI proxy)
10.202.64.180   *.apps wildcard VIP (MetalLB L2, advertised by a worker)
10.202.64.200+  DHCP pool for worker VMs
```

Plus one hosting-cluster MetalLB VIP for `<infra>-proxy-external` (OAuth).

## Public OAuth VIP

The minimal delta that puts OAuth on a public VIP while HyperShift keeps
`oauthServer.type: Route` (see
[Public DNS and OAuth publishing](../guides/public-dns-oauth.md)):

```yaml
infraComponents:
  proxy:
    externalService:
      enabled: true
      addressPoolName: metallb-pool
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
| `config/samples/hostedcluster_v1alpha1_infra.yaml` | Bare Infra scaffold |
| `config/samples/hostedcluster_v1alpha1_dhcpserver.yaml` etc. | Individual child CRs |
| `config/samples/species-8472-external-dns.yaml` | Hosted-kubeconfig ExternalDNS Deployment |
| `config/samples/openshift-example.yaml` | OpenShift-flavored example |
