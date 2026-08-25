# Apps ingress for hosted clusters

`InfraClusterAttachment.spec.appsIngress.enabled: true` enables wildcard
`*.apps.<cluster>.<baseDomain>` routing for one HostedCluster. The workflow is
attachment-owned:

1. The controller reads the HostedCluster kubeconfig named by
   `.status.kubeconfig.name`.
2. It waits for at least one Ready hosted worker so the MetalLB bundle can
   schedule.
3. It ensures the Red Hat MetalLB Operator Subscription, `MetalLB`,
   `IPAddressPool`, and `L2Advertisement` in the hosted cluster.
4. It creates the hosted `openshift-ingress/oooi-ingress` LoadBalancer Service
   by default, selecting the default IngressController.
5. It records the Service IP or hostname in
   `InfraClusterAttachment.status.appsIngressStatus`.
6. The shared Infra controller adds Envoy wildcard routes after a usable IP or
   hostname endpoint is reported. It adds static VLAN DNS only when the endpoint
   includes an IP address; hostname-only endpoints do not produce an A record.

The application TLS session terminates at the hosted ingress router. Envoy only
provides reachability to that router and continues to pass through control-plane
TLS.

## Example

```yaml
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
  appsIngress:
    enabled: true
    metallb:
      addressPoolName: apps-pool
      ipAddressPoolRange: 192.0.2.200-192.0.2.220
      # l2AdvertisementName: advertise-apps-pool
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

The `ipAddressPoolRange` is documentation and validation input for the
attachment. It must be unused, routable, and on the same Layer-2 network as the
hosted worker interfaces. Keep it disjoint from the gateway, static oooi
addresses, DHCP pool, and every other attachment's MetalLB range.

`service.labels` and `service.annotations` are merged on every reconciliation.
The operator owns `metallb.universe.tf/address-pool`; set the pool through
`appsIngress.metallb.addressPoolName`, not through Service annotations.

## Status and verification

```bash
kubectl -n clusters get infraattachment example-hcp \
  -o jsonpath='{.status.appsIngressStatus.phase}{" "}{.status.appsIngressStatus.reason}{" ip="}{.status.appsIngressStatus.externalIP}{" host="}{.status.appsIngressStatus.externalHostname}{"\n"}'
kubectl --kubeconfig=<hosted-kubeconfig> -n openshift-operators \
  get subscription,csv,metallb,ipaddresspool,l2advertisement
kubectl --kubeconfig=<hosted-kubeconfig> -n openshift-ingress \
  get service oooi-ingress -o wide
```

Typical phases and reasons are:

| Phase | Reason | Meaning |
|---|---|---|
| `Pending` | `WaitingForHostedClusterNodes` | No Ready worker is available |
| `Pending` | `WaitingForMetalLBCRDs` | OLM has not supplied the MetalLB CRDs |
| `Pending` | `WaitingForExternalIP` | The Service has no endpoint yet |
| `Ready` | `ReconciliationSucceeded` | The endpoint is known; Envoy can use an IP or hostname, while static VLAN DNS requires an IP |
| `Degraded` | `HostedClusterAccessFailed`, `MetalLBInstallFailed`, `IngressServiceFailed`, or `ExternalIPDiscoveryFailed` | Read the status message and hosted-cluster events |

Confirm all applicable paths:

```bash
# VLAN view
dig @192.0.2.3 +short console-openshift-console.apps.example-hcp.clusters.example.com

# Public view, when ExternalDNS is configured
dig +short console-openshift-console.apps.example-hcp.clusters.example.com @<public-resolver>
```

When `externalIP` is populated, the VLAN answer should be that IP and the
pod-network view uses the shared proxy Service ClusterIP. If only
`externalHostname` is populated, Envoy can target that hostname, but oooi does
not create a VLAN A record; provide a separate record or use an IP-backed
MetalLB endpoint.

## Public DNS ownership

oooi writes only VLAN-side records. It does not write Route53, Azure DNS, or
other public records. Run ExternalDNS where it can watch the hosted
`oooi-ingress` Service, or create/update the wildcard record from
`status.appsIngressStatus.externalIP` yourself. A management-cluster
ExternalDNS instance cannot see a Service in the hosted cluster without a
hosted-cluster kubeconfig.

## Cleanup

The apps-ingress objects are in the hosted cluster and are not owned by the
management-cluster `Infra`. Delete the attachment before deleting its Infra so
the finalizer can delete the configured hosted Service, `IPAddressPool`,
`L2Advertisement`, `MetalLB`, and Subscription by name, plus the control-plane
NetworkPolicy. It does not remove OLM CSVs, InstallPlans, operator workloads,
or public DNS records. These names are not ownership-checked, so dedicate them
to oooi and inspect/remove leftovers manually.

## Limitations

- Only the default OpenShift IngressController is selected.
- The documented flow is IPv4 and MetalLB L2 advertisement; validate BGP and
  dual-stack separately.
- oooi does not configure the physical network, upstream L2 switches, or public
  DNS.
- One DHCP authority must serve the VLAN.
