# Troubleshooting

Symptom-first guidance. Start with the [quick health snapshot](index.md#quick-health-snapshot),
then find your symptom below.

## Worker VMs never get an IP

| Check | Command | Fix |
|---|---|---|
| DHCP pod running | `kubectl -n clusters get pods -l app=proxy-server` and `<infra>-dhcp` pods | Read pod events; check NAD name/namespace |
| Only DHCP server on VLAN | Inspect upstream network config | Disable competing DHCP servers — oooi must be the sole authority |
| Static IP free | `arping 192.0.2.2` from VLAN | Pick unused `serverIP`s inside the CIDR |

## DNS resolution failures

**From the VLAN:**

```bash
dig @192.0.2.3 api.<cluster>.<domain>     # no answer?
```

- *Timeout*: DNSServer not Ready or wrong `serverIP`. Check
  `componentStatus.dnsReady`.
- *Wrong answer (pod IP instead of VLAN IP)*: query reached CoreDNS from a
  non-VLAN source, or `cidr` does not match actual client addresses.
- *Forwarded names fail*: `networkConfig.dnsServers` unreachable from the
  VLAN/pods; try explicit IPs instead of `"resolv.conf"`.

Watch live queries while testing:

```bash
kubectl -n clusters logs deploy/<infra>-dns -f
```

## Hosted cluster stuck installing

Workers bootstrap via Ignition through Envoy — if `ignition.<cluster>…` does
not resolve on the VLAN, nodes hang at `NotReady`.

1. Confirm `Infra` was applied **before** nodes booted.
2. From the VLAN: `curl -k https://ignition.<cluster>.<domain>` → expect any
   HTTP status (404 is fine); a timeout means the proxy path is broken.
3. Check ProxyServer logs for SNI routing errors:
   `kubectl -n clusters logs deploy/<infra>-proxy -c manager`.

## Apps ingress stuck in Pending

| Reason | Cause | Action |
|---|---|---|
| `WaitingForHostedClusterNodes` | No Ready worker yet | Wait; verify NodePool replicas |
| `WaitingForMetalLBCRDs` | OLM still installing MetalLB | `kubectl --kubeconfig=<kc> -n openshift-operators get sub,csv` |
| `WaitingForExternalIP` | MetalLB hasn't assigned VIP | Check pool range is L2-reachable & unused; speaker DaemonSet on workers |

Inspect deeper:

```bash
kubectl --kubeconfig=<hosted-kubeconfig> -n openshift-operators \
  get subscription,csv,metallb,ipaddresspool,l2advertisement
kubectl --kubeconfig=<hosted-kubeconfig> -n openshift-ingress get svc oooi-ingress -o wide
```

If `ipAddressPoolRange` overlaps gateway/statics/DHCP pool, allocation fails or
the VIP is unreachable — fix by editing the Infra to use a disjoint range.

## Console / canary route health fails after working

A classic **stale public wildcard record**: the ingress operator resolves
`*.apps.…` through public DNS; if it points at an old VIP after re-allocation,
routes fail with `RouteHealth_FailedGet`.

- Re-check `.status.appsIngressStatus.externalIP` vs `dig …@<public-resolver>`.
- With ExternalDNS (`--policy=sync`) records self-heal; without it, update the
  A record manually.

## OAuth returns timeouts publicly but works on VLAN

The public record must point at the **hosting-cluster** external Service VIP,
not the hosted Route. Verify:

```bash
kubectl -n <infra-namespace> get svc <infra>-proxy-external   # EXTERNAL-IP
dig +short oauth.<cluster>.<domain> @<public-resolver>         # must match above
curl -k -o /dev/null -w '%{http_code}\n' \
  'https://oauth.<cluster>.<domain>/oauth/authorize?client_id=openshift-challenging-client&response_type=token'  # 401 = good
```

## Proxy pods crash-loop binding ports

Envoy binds privileged ports (<1024) such as 443/80.

```bash
kubectl -n clusters get pod <proxy-pod> -o jsonpath='{.spec.serviceAccountName}'
kubectl get clusterrolebinding | grep <infra>
```

With `--enable-openshift=true`, oooi creates the scoped `privileged` SCC
RoleBinding automatically. If you disabled OpenShift integration, grant `anyuid`
(or a custom SCC) to the component ServiceAccounts yourself.

## Operator hot-looping on Services

Older oooi builds could fight LoadBalancer status updates. Ensure you run a
recent image; the reconciler now ignores LB status/metadata events and retains
allocated NodePorts. If you observe endless updates:

```bash
kubectl -n oooi-system logs deploy/oooi-controller-manager --tail=200 | grep -i service
kubectl -n oooi-system get deploy -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'
```

Upgrade via [Installation → Upgrade](../installation/index.md#upgrade).

## Deleting Infra leaves children behind

Owner-reference GC requires the API server to see the children as owned.
If CRDs were removed before deleting `Infra` instances, children vanish with
the CRDs but leftover RBAC may remain. Follow the supported order in
[Uninstall and cleanup](uninstall.md).
