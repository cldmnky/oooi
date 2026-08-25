# Static IPAM configuration

oooi uses the CNI static IPAM plugin to assign fixed secondary-network
addresses to its infrastructure pods. One `Infra` describes the shared VLAN;
one `InfraClusterAttachment` is required for each HostedCluster that should
contribute DNS and proxy routes.

## NetworkAttachmentDefinition

With static IPAM, the NAD does not contain a pool. The operator supplies each
component IP in its generated Multus annotation:

```yaml
apiVersion: k8s.cni.cncf.io/v1
kind: NetworkAttachmentDefinition
metadata:
  name: tenant-vlan-100
  namespace: default
spec:
  config: |
    {
      "cniVersion": "0.3.1",
      "type": "ipvlan",
      "name": "tenant-vlan-100",
      "master": "eth0",
      "mode": "l2",
      "ipam": {
        "type": "static"
      }
    }
```

Remove `subnet`, `gateway`, `rangeStart`, `rangeEnd`, and pool routes from a
host-local configuration. The NAD's namespace must match
`spec.networkConfig.networkAttachmentNamespace`, or the Infra namespace when
that field is omitted.

## Generated annotation

For example, a component with `serverIP: 192.168.100.2` receives:

```yaml
k8s.v1.cni.cncf.io/networks: |-
  [
    {
      "name": "tenant-vlan-100",
      "namespace": "default",
      "ips": ["192.168.100.2/24"]
    }
  ]
```

The operator adds this annotation to the DHCP, DNS, and proxy Deployments. DNS
and proxy preserve a supplied CIDR suffix and otherwise use `/24`; DHCP uses
the prefix length from the Infra network CIDR.

## Current resource model

The component child CRs are generated from `Infra` and are owned by it. DHCP
can be reconciled from the shared Infra alone, but DNS and Proxy require at
least one valid attachment because their static records and SNI backends are
attachment-specific.

```yaml
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: Infra
metadata:
  name: tenant-vlan-100
  namespace: clusters
spec:
  networkConfig:
    cidr: 192.168.100.0/24
    gateway: 192.168.100.1
    networkAttachmentDefinition: tenant-vlan-100
    networkAttachmentNamespace: default
    dnsServers:
      - 198.51.100.53
  infraComponents:
    dhcp:
      serverIP: 192.168.100.2
      rangeStart: 192.168.100.100
      rangeEnd: 192.168.100.200
      leaseTime: 1h
    dns:
      serverIP: 192.168.100.3
    proxy:
      serverIP: 192.168.100.4
      internalProxyService: tenant-vlan-100-proxy.clusters.svc.cluster.local
---
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: InfraClusterAttachment
metadata:
  name: example-hcp
  namespace: clusters
spec:
  infraRef:
    name: tenant-vlan-100
  hostedClusterRef:
    name: example-hcp
    namespace: clusters
  dns:
    clusterName: example-hcp
    baseDomain: clusters.example.com
```

The generated child names are `tenant-vlan-100-dhcp`,
`tenant-vlan-100-dns`, and `tenant-vlan-100-proxy` in the `clusters` namespace.

## Verification

```bash
kubectl -n clusters get pod -l app=dhcp-server -o wide
kubectl -n clusters get pod -l app=dns-server -o wide
kubectl -n clusters get pod -l app=proxy-server -o wide

kubectl -n clusters get pod <proxy-pod> \
  -o jsonpath='{.metadata.annotations.k8s\\.v1\\.cni\\.cncf\\.io/network-status}{"\\n"}'
```

The `network-status` annotation should show the requested secondary IP. Also
confirm that each static IP is unused on the VLAN and that the NAD exists:

```bash
kubectl get net-attach-def -n default tenant-vlan-100
arping 192.168.100.2
arping 192.168.100.3
arping 192.168.100.4
```

## Troubleshooting

- `NetworkAttachmentDefinition not found`: verify the NAD name and namespace in
  the Infra resource.
- IPAM rejects the address: confirm the IP has a CIDR suffix in the generated
  annotation and is inside the NAD's Layer-2 network.
- Pods remain in `ContainerCreating`: inspect pod events and confirm the static
  CNI plugin is installed on every eligible node.
- An address is already in use: choose new component addresses and remove any
  stale interface or workload holding the old address.

The static IPAM plugin does not configure the gateway, VLAN switch, routes, or
source-IP anti-spoofing. Those remain network-administrator responsibilities.
