# Apps ingress and MetalLB

With `InfraClusterAttachment.spec.appsIngress.enabled: true`, oooi turns on wildcard
`*.apps.<cluster>.<domain>` routing for the hosted cluster: MetalLB is
installed *inside the hosted cluster*, a LoadBalancer endpoint fronts the
default IngressController, and Envoy is wired to it. An IP-backed endpoint also
gets a VLAN DNS A record; the pod-network DNS answer is added when
`infraComponents.proxy.internalProxyService` is configured.

The feature is intentionally separate from the control-plane proxy endpoints:
app traffic terminates at the hosted ingress router (TLS by Routes), while
Envoy only provides reachability from both networks.

## What oooi does, in order

1. **Waits for a Ready hosted worker.** OLM's MetalLB bundle-unpack Job needs a
   schedulable node; starting earlier would time out.
2. **Installs the Red Hat MetalLB Operator** — Subscription in hosted-cluster
   namespace `openshift-operators` (`redhat-operators`, channel `stable`,
   automatic approval).
3. **Waits for MetalLB CRDs**, then ensures `MetalLB/metallb`, an
   `IPAddressPool`, and an `L2Advertisement` in `openshift-operators`.
4. **Creates/updates the LoadBalancer Service** (default
   `openshift-ingress/oooi-ingress`) with selector fixed to:
   `ingresscontroller.operator.openshift.io/deployment-ingresscontroller: default`
5. **Reads the allocated endpoint** from Service status and publishes it as
   `InfraClusterAttachment.status.appsIngressStatus.externalIP` /
   `.externalHostname`.
6. **Publishes Envoy backends** for `*.apps.<cluster>.<domain>` after a real
   endpoint exists. Static VLAN and pod-network A records require
   `externalIP`; a hostname-only endpoint is used by Envoy but does not create
   an oooi-generated A record.

```mermaid
sequenceDiagram
    participant I as Attachment controller
    participant H as Hosted cluster
    participant M as MetalLB
    participant D as DNSServer/Envoy
    I->>H: wait ≥1 Ready worker
    I->>H: Subscription metallb-operator
    I->>M: MetalLB + IPAddressPool + L2Advertisement
    I->>H: Service oooi-ingress (LoadBalancer)
    M-->>I: Endpoint assigned (IP or hostname)
    I->>D: VLAN *.apps A record if IP; pod view if IP and configured
    I->>D: Envoy wildcard SNI backends :80/:443 → endpoint
```

## Configuration

```yaml
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
      ipAddressPoolRange: 192.0.2.200-192.0.2.220   # unused L2 space
      # l2AdvertisementName: advertise-apps-pool     # optional override
    service:
      name: oooi-ingress                # defaults shown
      namespace: openshift-ingress
      annotations:                      # merged every reconcile — ExternalDNS hook
        external-dns.alpha.kubernetes.io/hostname: "*.apps.example-hcp.clusters.example.com."
      labels:
        external-dns.example.com/publish: "yes"
    ports:
      http: 80
      https: 443
```

### IP planning rules

`ipAddressPoolRange` must be **unused, routable addresses on the same L2
network as the hosted worker interfaces** (MetalLB L2 mode advertises the VIP
from a worker). Do not include:

- the VLAN gateway,
- DHCP/DNS/proxy static IPs,
- the DHCP dynamic pool,
- any other load balancer's range.

### Service metadata rules

- `service.labels` / `service.annotations` are **merged** onto the Service at
  every reconcile; unrelated existing metadata is preserved.
- The annotation `metallb.universe.tf/address-pool` is **operator-owned**
  (set from `metallb.addressPoolName`). Never put it in
  `service.annotations`.
- Trailing dots in hostname annotations (`…example.com.`) mark FQDNs for
  ExternalDNS.

## Status phases

| Phase | Reason | Meaning |
|---|---|---|
| `Pending` | `WaitingForHostedClusterNodes` | No Ready hosted worker yet |
| `Pending` | `WaitingForMetalLBCRDs` | Subscription installing |
| `Pending` | `WaitingForExternalIP` | MetalLB has not assigned the VIP yet (re-checks every 15s) |
| `Ready` | `ReconciliationSucceeded` | Endpoint known, Envoy wired; static DNS requires an IP and pod view is conditional |
| `Degraded` | `HostedClusterAccessFailed`, `MetalLBInstallFailed`, `IngressServiceFailed`, `ExternalIPDiscoveryFailed` | See `.message`; requeued |

Query:

```bash
kubectl -n clusters get infraattachment example-hcp \
  -o jsonpath='{.status.appsIngressStatus.phase}{" "}{.status.appsIngressStatus.reason}{" ip="}{.status.appsIngressStatus.externalIP}{"\n"}'
```

For degraded states inspect the hosted cluster through its admin kubeconfig:

```bash
kubectl --kubeconfig=<hosted-kubeconfig> -n openshift-operators \
  get subscription,csv,metallb,ipaddresspool,l2advertisement
kubectl --kubeconfig=<hosted-kubeconfig> -n openshift-ingress get svc oooi-ingress -o wide
```

## Verification

An allocated endpoint alone proves little. For an IP-backed endpoint, always
verify the VLAN path; verify the other paths only when they are configured:

1. **VLAN → VIP**: when `externalIP` is populated, `dig @<dns.serverIP>
   foo.apps.<cluster>.<domain>` returns that IP; a request to a hosted
   application route returns its expected response.
2. **Pod network → proxy ClusterIP**: when `internalProxyService` is configured,
   the same query against the DNS ClusterIP returns the proxy ClusterIP.
3. **Public → VIP**: when an ExternalDNS/provider path is configured, query your
   public resolver.

With a hostname-only endpoint, oooi does not publish the wildcard A record in
its DNS views. Verify that the hostname resolves from the proxy and that your
external DNS/provider path publishes the application name as appropriate.

Details and expected values: [Verification](../operations/verify.md).

## Limitations

- Only the **default** IngressController can be selected; custom selectors are
  not configurable today.
- IPv4 / L2-advertisement based. Validate dual-stack and BGP designs separately.
- Hostname-only LoadBalancer endpoints are valid Envoy targets but do not yield
  oooi-generated wildcard A records; use an IP-backed endpoint for split-horizon
  DNS answers.
- Public DNS records are never written by oooi itself — see the next guide.
