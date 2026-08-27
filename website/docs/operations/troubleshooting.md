# Troubleshooting

Symptom-first guidance. Start with the [quick health snapshot](index.md#quick-health-snapshot),
then find your symptom below.

## Worker VMs never get an IP

| Check | Command | Fix |
|---|---|---|
| DHCP pod running | `kubectl -n clusters get pods -l app=dhcp-server` | Read pod events; check NAD name and namespace |
| Only DHCP server on VLAN | Inspect the upstream network configuration | Disable competing DHCP servers; oooi must be the sole authority |
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

### Kubernetes alias is missing or selects no backend

The four unqualified aliases are conditional. Check that a matching KubeVirt
NodePool exists and that its CAPI Machines have an address inside the shared
Infra CIDR:

```bash
kubectl -n <hosted-cluster-namespace> get nodepool
kubectl -n <control-plane-namespace> get machine -o yaml
kubectl -n <infra-namespace> get proxyserver <infra>-proxy \
  -o jsonpath='{range .spec.backends[?(@.name=="<attachment>-kubernetes-hostname")].sourcePrefixRanges}{.}{"\n"}{end}'
kubectl -n <infra-namespace> get proxyserver <infra>-proxy \
  -o jsonpath='{range .spec.backends[?(@.name=="<attachment>-kubernetes-service")].sourcePrefixRanges}{.}{"\n"}{end}'
```

Addresses outside `networkConfig.cidr` are deliberately ignored. A pending
Machine status causes the alias backends to be omitted while the fully
qualified `api.<cluster>.<domain>` route remains available. The `hostname`
backend serves SNI clients on port `443`; the `service` backend serves the
IP-based no-SNI path on port `6443`. If two attachments claim the same `/32`,
inspect the Infra `Ready` condition for `DuplicateSourceIP`; only the
conflicting alias routes are suppressed.

## Hosted cluster stuck installing

Workers bootstrap through Envoy to Ignition. If
`ignition.<cluster>.<domain>` does not resolve on the VLAN, nodes remain
`NotReady`.

1. Confirm `Infra` was applied **before** nodes booted.
2. From the VLAN, run `curl -k https://ignition.<cluster>.<domain>`. Any HTTP
   status, including `404`, confirms that the proxy path responds. A timeout
   indicates that the proxy path is unavailable.
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

If `ipAddressPoolRange` overlaps the gateway, static addresses, or DHCP pool,
allocation can fail or the VIP can be unreachable. Edit the relevant
`InfraClusterAttachment.spec.appsIngress.metallb.ipAddressPoolRange` to use a
disjoint range:

```bash
kubectl -n <infra-namespace> edit infraattachment <attachment>
```

## Console / canary route health fails after working

A stale public wildcard record can cause this failure. The ingress Operator
resolves `*.apps.<cluster>.<domain>` through public DNS. If the record points to
an old VIP after reallocation, routes fail with `RouteHealth_FailedGet`.

- Compare the attachment's `.status.appsIngressStatus.externalIP` with a query to your public
  resolver. If only `.externalHostname` is populated, verify the hostname and
  provider record instead; oooi does not generate a VLAN A record for it.
- With ExternalDNS (`--policy=sync`) records self-heal; without it, update the
  A record manually.

## OAuth returns timeouts publicly but works on VLAN

The public record must point at the **hosting-cluster** external Service VIP,
not the hosted Route. Verify:

```bash
kubectl -n <infra-namespace> get svc <attachment>-proxy-external   # EXTERNAL-IP
dig +short oauth.<cluster>.<domain> @<public-resolver>         # must match above
curl -k -o /dev/null -w '%{http_code}\n' \
  'https://oauth.<cluster>.<domain>/oauth/authorize?client_id=openshift-challenging-client&response_type=token'  # 401 = good
```

## API returns timeouts publicly but works on VLAN

The public API record must point at the HostedCluster's management-cluster
`kube-apiserver` LoadBalancer Service in its control-plane namespace. Verify
the Service's current VIP and its hostname annotation:

```bash
kubectl -n <control-plane-namespace> get svc kube-apiserver -o wide
dig +short api.<cluster>.<domain> @<public-resolver>
curl -k https://api.<cluster>.<domain>:6443/version  # 200 = good
```

If the management ExternalDNS instance uses a publish label filter, add its
matching label to the API Service. A hosted-cluster ExternalDNS instance cannot
publish this management-cluster Service.

## Proxy pods crash-loop binding ports

Envoy binds privileged ports below `1024`, such as `443` and `80`.

```bash
kubectl -n clusters get pod <proxy-pod> -o jsonpath='{.spec.serviceAccountName}'
kubectl -n clusters get rolebinding <infra>-proxy-privileged-scc
```

With `--enable-openshift=true`, oooi creates the scoped `privileged` Security
Context Constraints (SCC) RoleBinding automatically. If OpenShift integration
is disabled, grant an equivalent custom SCC to the component ServiceAccounts.

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
