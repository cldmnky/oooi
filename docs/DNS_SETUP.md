# Split-horizon DNS setup

oooi's `DNSServer` runs CoreDNS with two source-based views for a shared
secondary network:

- VLAN clients receive the Envoy secondary-network IP.
- Management-cluster clients receive the proxy Service ClusterIP when
  `internalProxyService` is configured.
- Names outside oooi's static records are forwarded to the configured upstream
  resolvers.

The Infra controller creates one shared `DNSServer` for each `Infra`. The
cluster-specific names come from one `InfraClusterAttachment` per
HostedCluster.

## Configuration

The `Infra` owns the DNS server address, the VLAN CIDR, and upstream resolvers.
The attachment owns the cluster domain:

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
    networkAttachmentNamespace: default
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
  name: example-hcp
  namespace: clusters
spec:
  infraRef:
    name: tenant-vlan100
  hostedClusterRef:
    name: example-hcp
    namespace: clusters
  dns:
    clusterName: example-hcp
    baseDomain: clusters.example.com
```

If `networkAttachmentNamespace` is omitted, oooi uses the `Infra` namespace.
`dnsServers` is only for forwarding non-static names; when DNS is enabled, DHCP
advertises `infraComponents.dns.serverIP` to workers automatically.

Use the literal `resolv.conf` when CoreDNS should inherit the node resolver:

```yaml
spec:
  networkConfig:
    dnsServers:
      - resolv.conf
```

## Generated records

For every valid attachment, the shared DNS server generates:

```text
api.<cluster>.<baseDomain>
api-int.<cluster>.<baseDomain>
oauth.<cluster>.<baseDomain>
ignition.<cluster>.<baseDomain>
konnectivity.<cluster>.<baseDomain>
```

From the VLAN these records point to `proxy.serverIP`. From the pod-network
view they point to the resolved ClusterIP named by `internalProxyService`.
When apps ingress is Ready and reports an `externalIP`,
`*.apps.<cluster>.<baseDomain>` points to that MetalLB address in the VLAN view
and to the proxy ClusterIP in the pod-network view. A hostname-only endpoint can
be used by Envoy, but does not produce an oooi-generated wildcard A record.

### Kubernetes service aliases

For KubeVirt workers, the shared DNS server also publishes these four names to
the shared VLAN proxy IP when at least one usable worker source range exists:

```text
kubernetes
kubernetes.default
kubernetes.default.svc
kubernetes.default.svc.cluster.local
```

The aliases have one DNS answer because all attachments share one proxy IP.
Envoy selects the control plane from the source IP of the connection. The Infra
controller obtains worker addresses from CAPI `Machine.status.addresses`,
associated with KubeVirt NodePools by the
`hypershift.openshift.io/nodePool` annotation, and retains only addresses in
`networkConfig.cidr`. Each retained address becomes a `/32` source range.

If Machine addresses have not propagated yet, the aliases are not emitted. The
fully qualified records remain independent of alias discovery. If two
attachments claim the same source address, only their ambiguous alias routes
are suppressed and the Infra reports `DuplicateSourceIP`.

Source-IP matching is not authentication. A client able to spoof another
worker's VLAN address can select that attachment; enforce anti-spoofing in the
CNI or switching layer where necessary.

## How the views work

```text
VLAN client source in networkConfig.cidr
  -> multus view
  -> proxy.serverIP for static HCP and alias records

Management-cluster pod source outside the CIDR
  -> default view
  -> internal proxy ClusterIP for static HCP records, if configured

Any other name
  -> configured upstream resolvers
```

The generated Corefile is stored in the `DNSServer` ConfigMap and reloads on
the configured interval. The DNSServer status exposes the ConfigMap, Deployment,
Service, and Service ClusterIP names.

## Verification

From a VLAN client, query the static proxy address:

```bash
dig @192.0.2.3 +short api.example-hcp.clusters.example.com
# 192.0.2.4

dig @192.0.2.3 +short kubernetes.default.svc
# 192.0.2.4 when a KubeVirt Machine source address is ready

dig @192.0.2.3 +short www.example.net
# Answer from the configured upstream resolver
```

From the pod network, query the DNSServer Service ClusterIP:

```bash
DNSCLUSTERIP=$(kubectl -n clusters get svc tenant-vlan100-dns \
  -o jsonpath='{.spec.clusterIP}')
kubectl run dnstest --rm -it --restart=Never \
  --image=registry.example.com/diagnostics:latest -- \
  nslookup api.example-hcp.clusters.example.com "$DNSCLUSTERIP"
```

The pod-network result should be the proxy Service ClusterIP, not the VLAN
address. Use an image that contains `nslookup`.

Inspect generated records and status with:

```bash
kubectl -n clusters get dnsserver tenant-vlan100-dns -o yaml
kubectl -n clusters get configmap tenant-vlan100-dns-dns-config \
  -o jsonpath='{.data.Corefile}'
kubectl -n clusters logs deploy/tenant-vlan100-dns
```

For a missing alias, inspect the NodePool, Machine addresses, and generated
`ProxyServer.spec.backends[*].sourcePrefixRanges` before debugging DNS. See
the [website verification guide](../website/docs/operations/verify.md) and
[troubleshooting guide](../website/docs/operations/troubleshooting.md).

## OpenShift DNS forwarding

If cluster-wide pod DNS should forward the hosted domain to oooi, configure the
OpenShift DNS operator with the `DNSServer` Service ClusterIP:

```yaml
apiVersion: operator.openshift.io/v1
kind: DNS
metadata:
  name: default
spec:
  servers:
    - name: hosted-cluster-dns
      zones:
        - clusters.example.com
      forwardPlugin:
        policy: Random
        upstreams:
          - 198.18.0.10:53
```

Replace the example upstream with the current `DNSServer` Service ClusterIP.
This is optional; direct queries to the DNSServer Service are sufficient for
diagnostics.

## Common failures

- A timeout usually means the DNS Deployment is not Ready, the NAD is wrong,
  or `serverIP` is not reachable from the VLAN.
- A pod-network query returning the VLAN IP means the query did not reach the
  intended default view or the client source is being seen incorrectly.
- A missing alias usually means CAPI Machine status has no address inside the
  Infra CIDR yet. Fully qualified names should still resolve.
- Forwarded names fail when `dnsServers` are unreachable; use explicit resolver
  IPs or `resolv.conf` as appropriate.
- A stale public `*.apps` record is an ExternalDNS/provider issue, not a
  DNSServer issue. Compare it with `appsIngressStatus.externalIP`, or verify
  `externalHostname` when the endpoint is hostname-based.
