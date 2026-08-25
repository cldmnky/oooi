# Verification

A running oooi deployment has one required path and two optional integration
paths. An allocated VIP alone is not sufficient; verify each path that your
configuration enables.

| # | Path | When | Proves |
|---|---|---|---|
| 1 | VLAN client → VLAN IPs | Always | Workers can bootstrap and operate |
| 2 | Management pod → ClusterIPs | `internalProxyService` configured | Pod-network split-horizon view works |
| 3 | Public resolver → VIPs | External Service/apps ingress and ExternalDNS configured | External clients reach OAuth / apps |

## 1. Kubernetes status

```bash
kubectl -n clusters get infra <name>
kubectl -n clusters get infra <name> \
  -o jsonpath='ready={.status.conditions[?(@.type=="Ready")].status}{"\n"}'
kubectl -n clusters get infra <name> \
  -o jsonpath='{.status.componentStatus}{"\n"}'
kubectl -n clusters get infraattachment <name> \
  -o jsonpath='{.status.appsIngressStatus.phase}{" "}{.status.appsIngressStatus.reason}{" ip="}{.status.appsIngressStatus.externalIP}{"\n"}'
kubectl -n clusters get dhcpserver,dnsserver,proxyserver
kubectl -n clusters rollout status deployment/<name>-dhcp --timeout=5m
kubectl -n clusters rollout status deployment/<name>-dns --timeout=5m
kubectl -n clusters rollout status deployment/<name>-proxy --timeout=5m
```

The `Infra` condition and component-status fields confirm reconciliation, not
Deployment availability. A successful `rollout status` confirms each enabled
component is available.

## 2. From the VLAN

Use a real VLAN client or a VLAN-attached probe. With DNS at `.3`:

```bash
dig @192.0.2.3 +short api.example-hcp.clusters.example.com
# → proxy serverIP, e.g. 192.0.2.4

dig @192.0.2.3 +short kubernetes.default.svc
# → the same proxy serverIP when a KubeVirt Machine source address is ready

dig @192.0.2.3 +short console-openshift-console.apps.example-hcp.clusters.example.com
# → apps VIP, e.g. 192.0.2.200
```

HTTP checks and expected results:

```bash
curl -k https://api.example-hcp.clusters.example.com:6443/version          # 200
curl -k -o /dev/null -w '%{http_code}\n' \
  'https://oauth.example-hcp.clusters.example.com/oauth/authorize?client_id=openshift-challenging-client&response_type=token'
# 401 — unauthenticated reach proves the TLS/SNI path to the OAuth backend
curl -k -o /dev/null -w '%{http_code}\n' \
  https://console-openshift-console.apps.example-hcp.clusters.example.com  # 200
```

| Endpoint | Status | Meaning |
|---|---|---|
| API `/version` | `200` | SNI passthrough to kube-apiserver works |
| OAuth `/oauth/authorize` | `401` | Reached the hosted OAuth server; auth required as expected |
| Console | `200` | Wildcard VIP → ingress router works |
| Ignition `/` | `404` | Normal at the root path; backend reached |
| konnectivity plain HTTP | `415` | Backend reached; expects a different protocol |

For a source-scoped alias, verify the generated ranges before interpreting a
failure as a DNS failure:

```bash
kubectl -n clusters get proxyserver <infra>-proxy \
  -o jsonpath='{range .spec.backends[?(@.name=="<attachment>-kubernetes-hostname")].sourcePrefixRanges}{.}{"\n"}{end}'
```

The result should contain the worker's VLAN address as a `/32`. Addresses are
not populated until CAPK has copied the VMI interface address to CAPI Machine
status. Check the fully qualified API name while that propagation is pending.

## 3. From the pod network (optional)

Run this section only when `internalProxyService` is configured. The names must
then resolve to the **proxy Service ClusterIP**:

```bash
export DIAGNOSTICS_IMAGE=registry.example.com/diagnostics:latest
DNSCLUSTERIP=$(kubectl -n clusters get svc example-hcp-dns \
  -o jsonpath='{.spec.clusterIP}')
kubectl run dnstest --rm -it --restart=Never \
  --image="$DIAGNOSTICS_IMAGE" -- \
  sh -c "nslookup api.example-hcp.clusters.example.com $DNSCLUSTERIP"
```

Set `DIAGNOSTICS_IMAGE` to an image in your registry that provides `nslookup`.

## 4. Public resolution (optional)

Run this section only after an ExternalDNS/provider path is configured. The apps
query also requires apps ingress; the OAuth query requires the proxy external
Service. Once the applicable ExternalDNS record has converged:

```bash
dig +short console-openshift-console.apps.example-hcp.clusters.example.com @<public-resolver>
# → current InfraClusterAttachment.status.appsIngressStatus.externalIP, when IP-backed

dig +short oauth.example-hcp.clusters.example.com @<public-resolver>
# → current <attachment>-proxy-external EXTERNAL-IP
```

Also verify ownership TXT records exist for your registry (prevents two
ExternalDNS instances fighting over the same names).

## 5. Browser login (optional public path)

If public apps and OAuth publishing are configured, open the hosted console URL
in a browser, choose your identity provider, and log in. A successful login
exercises: public DNS → MetalLB VIP → Envoy SNI → OAuth Route → console route.
