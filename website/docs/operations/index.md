# Operations

Day-2 guidance for running oooi-managed hosted clusters.

<div class="grid cards" markdown>

-   :material-checkbox-marked-circle-outline: __Verification__

    ---

    Ready-made checks for all three paths: VLAN, pod network, public DNS.

    [Verify a deployment](verify.md)

-   :wrench: __Troubleshooting__

    ---

    Symptom → cause → fix for the most common failure modes.

    [Debug problems](troubleshooting.md)

-   :material-delete-outline: __Uninstall and cleanup__

    ---

    Supported teardown order, garbage collection, and orphan removal.

    [Tear down safely](uninstall.md)

</div>

## Quick health snapshot

```bash
# Overall readiness
kubectl -n clusters get infra

# Per-component readiness
kubectl -n clusters get infra <name> \
  -o jsonpath='{.status.componentStatus}{"\n"}'

# Apps ingress state and VIP for one attachment
kubectl -n clusters get infraattachment <name> \
  -o jsonpath='{.status.appsIngressStatus.phase}{" "}{.status.appsIngressStatus.reason}{" ip="}{.status.appsIngressStatus.externalIP}{"\n"}'

# Source ranges for one KubeVirt attachment's Kubernetes aliases
kubectl -n clusters get proxyserver <infra>-proxy \
  -o jsonpath='{range .spec.backends[?(@.name=="<attachment>-kubernetes-hostname")].sourcePrefixRanges}{.}{"\n"}{end}'

# Child workloads
kubectl -n clusters get dhcpserver,dnsserver,proxyserver
kubectl -n clusters get pods -l app.kubernetes.io/managed-by 2>/dev/null || \
  kubectl -n clusters get pods | grep -E 'dhcp|dns|proxy'
```

## Useful log locations

| Component | Where |
|---|---|
| Infra / apps-ingress controller | `oooi-system` manager Deployment |
| DHCP server | `clusters` namespace, `<infra>-dhcp-*` pod |
| CoreDNS query log | `clusters` namespace, `<infra>-dns-*` pod (`logs -f`) |
| Envoy + xDS manager | `clusters` namespace, `<infra>-proxy-*` pod (2 containers) |
