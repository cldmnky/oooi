# Envoy proxy setup

oooi runs Envoy as a Layer-4, TLS-passthrough gateway between an isolated VLAN
and HostedCluster Services on the management-cluster pod network. The proxy
reads the TLS ClientHello SNI value, selects a backend, and forwards encrypted
TCP bytes. It does not load certificates or terminate TLS.

In the normal workflow, `Infra` creates one shared `ProxyServer` and each
`InfraClusterAttachment` contributes its control-plane routes. A direct
`ProxyServer` is useful for component-level testing, but it does not provide
the attachment aggregation or source-scoped Kubernetes aliases.

## Network and listeners

The proxy pod has a default pod-network interface and a static Multus
interface. Its VLAN address is `infraComponents.proxy.serverIP`.

| Listener | Traffic | Matching |
|---|---|---|
| `443` | OAuth, Ignition, konnectivity, aliases, and apps HTTPS | TLS inspector plus SNI; aliases also require a source range |
| `6443` | API and API-int | Plain TCP for a single API backend; SNI-aware when multiple backends are present |
| `80` | Apps HTTP when enabled | Plain TCP |
| `9901` | Envoy admin and metrics | Pod-network Service only; not exposed by the external Service |
| `18000` | Manager-to-Envoy xDS | Pod-local manager sidecar connection |

The generated API routes are:

```text
api.<cluster>.<baseDomain>:6443          -> kube-apiserver:6443
api-int.<cluster>.<baseDomain>:6443      -> kube-apiserver:6443
oauth.<cluster>.<baseDomain>:443         -> oauth-openshift:6443
ignition.<cluster>.<baseDomain>:443      -> ignition-server-proxy:443
konnectivity.<cluster>.<baseDomain>:443  -> konnectivity-server:8091
```

The target Services run in the attachment's resolved control-plane namespace.
The default is `<HostedCluster namespace>-<HostedCluster name>`.

## Source-scoped Kubernetes aliases

The shared proxy supports these aliases for KubeVirt worker traffic:

```text
kubernetes
kubernetes.default
kubernetes.default.svc
kubernetes.default.svc.cluster.local
```

All aliases resolve to the shared VLAN proxy address. The Infra controller
builds one generated backend per eligible attachment and sets
`ProxyBackend.sourcePrefixRanges` to the worker `/32` addresses. Discovery uses
the management-cluster objects below:

1. A HyperShift `NodePool` identifies KubeVirt membership for the HostedCluster.
2. CAPI `Machine` objects carry the
   `hypershift.openshift.io/nodePool=<namespace>/<name>` annotation linking
   them to that pool.
3. CAPK mirrors VMI interface addresses to `Machine.status.addresses`.
4. oooi keeps only addresses inside `Infra.spec.networkConfig.cidr`,
   deduplicates and sorts them, and emits the source ranges. Validate CAPK's
   interface selection and address refresh behavior for the target release.

The NodePool and Machine watches are management-cluster watches. oooi does not
need a remote VMI informer for an external KubeVirt infrastructure cluster.
Aliases are omitted while a KubeVirt NodePool has no usable in-CIDR address and
are rebuilt after Machine address updates. The generated field is capped at
256 source ranges.

Source ranges select a route; they are not authentication. A privileged client
that spoofs another worker's VLAN address may select another attachment. Use
network-level anti-spoofing controls when the VLAN is not fully trusted.

## Generated configuration

An attachment is configured through `InfraClusterAttachment`, not by editing
the generated child:

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
```

Inspect the result with:

```bash
kubectl -n clusters get proxyserver tenant-vlan100-proxy
kubectl -n clusters get proxyserver tenant-vlan100-proxy \
  -o jsonpath='{range .spec.backends[*]}{.name}{" "}{.hostname}{" "}{.port}{"\n"}{end}'
kubectl -n clusters get proxyserver tenant-vlan100-proxy \
  -o jsonpath='{range .spec.backends[?(@.name=="example-hcp-kubernetes-hostname")].sourcePrefixRanges}{.}{"\n"}{end}'
```

The `ProxyServer` status `backendCount` describes successfully configured
backends. `Infra.status.componentStatus.proxyReady` describes reconciliation of
a valid child configuration, not Deployment availability; use `rollout status`
for the workload.

## Public hosting-cluster Service

Set `spec.externalService.enabled: true` on an `InfraClusterAttachment` to
create `<attachment>-proxy-external`, a hosting-cluster `LoadBalancer`
Service selecting the shared Envoy pod. Each attachment gets its own Service
and VIP. It exposes only the configured proxy ingress port, not `9901`, xDS, or
backend ports:

```yaml
spec:
  externalService:
    enabled: true
    addressPoolName: hosting-public-pool
    annotations:
      external-dns.alpha.kubernetes.io/hostname: oauth.example-hcp.clusters.example.com.
    labels:
      external-dns.example.com/publish: "yes"
```

`addressPoolName` becomes the standard MetalLB Service annotation. Labels and
annotations are reconciled by oooi. The hostname annotation is configured on
this attachment, so each cluster's OAuth record has its own Service and VIP.
oooi does not write public DNS records; use an ExternalDNS instance that
watches the hosting cluster Services.

## TLS and fallback behavior

The Envoy listener uses `tls_inspector` for SNI-based listeners. A TLS
connection with a fully qualified SNI name matches the attachment's backend.
The backend Service, not Envoy, presents the certificate.

The 443 listener also adds a legacy konnectivity fallback when a non-source-
scoped konnectivity backend exists. It is intended for clients that connect
without a hostname, but its nil filter-chain match is a true catch-all: an
unmatched SNI or source range can also reach that backend. Do not treat the
fallback as an alias route or as an access-control boundary. Alias clients must
use the required SNI and an address in the generated source range. Validate the
deployed Envoy version and inspect access logs before relying on unmatched
traffic behavior for a security policy.

## OpenShift permissions

Ports below 1024 require the OpenShift integration flag and the scoped
privileged SCC binding created by oooi:

```bash
go run ./main.go manager --enable-openshift=true
```

The proxy Deployment must also be attached to a working NAD with static IPAM.
The NAD name is `networkConfig.networkAttachmentDefinition`; its namespace is
`networkAttachmentNamespace`, or the Infra namespace when omitted. The static
IP is supplied through the generated Multus annotation.

Verify the pod network and SCC:

```bash
kubectl -n clusters get pod -l app=proxy-server -o wide
kubectl -n clusters get pod <proxy-pod> \
  -o jsonpath='{.metadata.annotations.k8s\.v1\.cni\.cncf\.io/network-status}'
kubectl -n clusters logs deploy/tenant-vlan100-proxy -c envoy
kubectl -n clusters logs deploy/tenant-vlan100-proxy -c manager
```

## Troubleshooting

### No route or wrong backend

Check the generated hostname, target namespace, and source ranges:

```bash
kubectl -n clusters get proxyserver tenant-vlan100-proxy -o yaml
kubectl -n <control-plane-namespace> get svc \
  kube-apiserver oauth-openshift ignition-server-proxy konnectivity-server
```

For an alias, check the CAPI Machine status and verify its address is inside
the Infra CIDR. A missing alias backend is expected until that address is
available. A `DuplicateSourceIP` Infra condition means two attachments claimed
the same address; fully qualified routes are unaffected.

Use SNI explicitly from a VLAN client:

```bash
openssl s_client -connect 192.0.2.4:443 \
  -servername api.example-hcp.clusters.example.com
curl -k --resolve api.example-hcp.clusters.example.com:6443:192.0.2.4 \
  https://api.example-hcp.clusters.example.com:6443/version
```

### Envoy or manager is not ready

```bash
kubectl -n clusters rollout status deployment/tenant-vlan100-proxy --timeout=5m
kubectl -n clusters logs deployment/tenant-vlan100-proxy -c manager --tail=100
kubectl -n clusters logs deployment/tenant-vlan100-proxy -c envoy --tail=100
kubectl -n clusters port-forward deployment/tenant-vlan100-proxy 9901:9901
curl http://127.0.0.1:9901/config_dump
```

The admin port is intended for an authenticated local port-forward or an
equivalent protected path; it is not part of the external LoadBalancer.

## References

- [Envoy listener filter-chain matching](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/advanced/matching/matching_listener)
- [TLS inspector](https://www.envoyproxy.io/docs/envoy/latest/configuration/listeners/listener_filters/tls_inspector)
- [OpenShift SCCs](https://docs.redhat.com/en/documentation/openshift_container_platform/latest/html/authentication/managing-pod-security-policies)
- [Multus CNI](https://github.com/k8snetworkplumbingwg/multus-cni)
- [Multiple hosted clusters](../website/docs/guides/multi-cluster.md)
