# Verification

A running oooi deployment has **three distinct paths** that must all work. An
allocated VIP alone is not sufficient — verify each path explicitly.

| # | Path | Proves |
|---|---|---|
| 1 | VLAN client → VLAN IPs | Workers can bootstrap and operate |
| 2 | Management pod → ClusterIPs | Pod-network split-horizon view works |
| 3 | Public resolver → VIPs | External clients reach OAuth / apps |

## 1. Kubernetes status

```bash
kubectl -n clusters get infra <name>
kubectl -n clusters get infra <name> \
  -o jsonpath='ready={.status.conditions[?(@.type=="Ready")].status}{"\n"}'
kubectl -n clusters get infra <name> \
  -o jsonpath='{.status.componentStatus}{"\n"}'
kubectl -n clusters get infra <name> \
  -o jsonpath='{.status.appsIngressStatus.phase}{" "}{.status.appsIngressStatus.reason}{" ip="}{.status.appsIngressStatus.externalIP}{"\n"}'
kubectl -n clusters get dhcpserver,dnsserver,proxyserver
```

All three child CRs should exist (for enabled components) with their
Deployments `Ready`.

## 2. From the VLAN

Use a real VLAN client or a VLAN-attached probe. With DNS at `.3`:

```bash
dig @10.202.64.3 +short api.species-8472.clusters.example.com
# → proxy serverIP, e.g. 10.202.64.4

dig @10.202.64.3 +short console-openshift-console.apps.species-8472.clusters.example.com
# → apps VIP, e.g. 10.202.64.180
```

HTTP checks and expected results:

```bash
curl -k https://api.species-8472.clusters.example.com:6443/version          # 200
curl -k -o /dev/null -w '%{http_code}\n' \
  'https://oauth.species-8472.clusters.example.com/oauth/authorize?client_id=openshift-challenging-client&response_type=token'
# 401 — unauthenticated reach proves the TLS/SNI path to the OAuth backend
curl -k -o /dev/null -w '%{http_code}\n' \
  https://console-openshift-console.apps.species-8472.clusters.example.com  # 200
```

| Endpoint | Status | Meaning |
|---|---|---|
| API `/version` | `200` | SNI passthrough to kube-apiserver works |
| OAuth `/oauth/authorize` | `401` | Reached the hosted OAuth server; auth required as expected |
| Console | `200` | Wildcard VIP → ingress router works |
| Ignition `/` | `404` | Normal at the root path; backend reached |
| konnectivity plain HTTP | `415` | Backend reached; expects a different protocol |

## 3. From the pod network

The same names must resolve to the **proxy Service ClusterIP**:

```bash
DNSCLUSTERIP=$(kubectl -n clusters get svc species-8472-dns \
  -o jsonpath='{.spec.clusterIP}')
kubectl run dnstest --rm -it --restart=Never \
  --image=registry.access.redhat.com/ubi9/ubi-minimal -- \
  sh -c "nslookup api.species-8472.clusters.example.com $DNSCLUSTERIP"
```

## 4. Public resolution

Once ExternalDNS has converged (typically ≤ 2 sync intervals):

```bash
dig +short console-openshift-console.apps.species-8472.clusters.example.com @1.1.1.1
# → current .status.appsIngressStatus.externalIP

dig +short oauth.species-8472.clusters.example.com @1.1.1.1
# → current <infra>-proxy-external EXTERNAL-IP
```

Also verify ownership TXT records exist for your registry (prevents two
ExternalDNS instances fighting over the same names).

## 5. Browser login (end-user check)

Open the hosted console URL in a browser, choose your identity provider, and
log in. A successful login exercises: public DNS → MetalLB VIP → Envoy SNI →
OAuth Route → console route — i.e., every layer this documentation covers.
