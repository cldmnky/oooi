# Apps Ingress for Hosted Clusters

Wildcard `*.apps.<cluster>.<tld>` routing via MetalLB + Envoy + split-horizon DNS.

## Summary

When `spec.appsIngress.enabled: true`, the operator:

1. Reads `status.kubeconfig.name` from `HostedCluster` (`hypershift.openshift.io/v1beta1`) in `spec.appsIngress.hostedClusterRef.{name,namespace}`.
2. Builds a kubeconfig client for the hosted cluster.
3. Ensures MetalLB operator (`operators.coreos.com/Subscription` `metallb-operator` `redhat-operators` → `metallb.io/MetalLB` + `IPAddressPool` + `L2Advertisement`) in `openshift-operators`.
4. Creates/updates `Service` `spec.appsIngress.service.{name,namespace}` (default `oooi-ingress`/`openshift-ingress`) type `LoadBalancer` with `metallb.universe.tf/address-pool` annotation, ports `spec.appsIngress.ports.{http,https}` (80/443).
5. Polls `.status.loadBalancer.ingress[0].{ip,hostname}` → `status.appsIngressStatus.externalIP` (for IP) or `status.appsIngressStatus.externalHostname` (for hostname).
6. DNS: adds `*.apps.<cluster>.<tld>` (derived from `spec.infraComponents.dns.{clusterName,baseDomain}` or `spec.appsIngress.baseDomain`) to `DNSServer` static entries. Split-horizon: VLAN view → MetalLB external IP, pod-network view → internal proxy IP.
7. Proxy: adds backends `apps-http`/`apps-https` (wildcard SNI) on 80/443 targeting external IP via Envoy STATIC/LOGICAL_DNS clusters. Port 80 uses plain TCP (no TLS inspector), 443 uses SNI.

Status: `status.appsIngressStatus.phase` `Pending` (`WaitingForExternalIP`, RequeueAfter 15s) → `Ready` (`ReconciliationSucceeded`) or `Degraded` (`HostedClusterAccessFailed`/`MetalLBInstallFailed`/`IngressServiceFailed`/`ExternalIPDiscoveryFailed`, RequeueAfter 30s or 15s).

## Example

```yaml
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: Infra
metadata:
  name: mycluster
  namespace: clusters
spec:
  networkConfig:
    cidr: "192.168.100.0/24"
    gateway: "192.168.100.1"
    networkAttachmentDefinition: "vlan100"
  infraComponents:
    dns:
      enabled: true
      serverIP: "192.168.100.3"
      clusterName: "mycluster"
      baseDomain: "example.com"
    proxy:
      enabled: true
      serverIP: "192.168.100.10"
  appsIngress:
    enabled: true
    baseDomain: "example.com"  # optional, defaults to dns.baseDomain; wildcard uses *.apps.<clusterName>.<baseDomain>
    hostedClusterRef:
      name: "mycluster"
      namespace: "clusters"
    metallb:
      addressPoolName: "lab-network"
      ipAddressPoolRange: "10.202.64.221-10.202.64.240"
      l2AdvertisementName: "advertise-lab-network" # optional
    service:
      name: "oooi-ingress"
      namespace: "openshift-ingress"
    ports:
      http: 80
      https: 443
```

Result: `*.apps.mycluster.example.com` → VLAN 10.202.64.221, pod-network → proxy ClusterIP → MetalLB.

## DNS / Proxy Details

- DNS wildcard is added only when `phase: Ready` (external IP known). Requires `spec.infraComponents.dns` to be configured for hostedClusterDomain.
- Proxy wildcard backends use direct IP/hostname endpoint: Envoy `STATIC` for IP, `LOGICAL_DNS` for hostname. Ensure proxy pod has VLAN reachability to external IP (policy-based routing via NetworkPolicy `allow-infrastructure`).
- Verify: `dig @<dnsServerIP> foo.apps.mycluster.example.com` (from VLAN vs pod), `curl -k https://foo.apps.mycluster.example.com` via proxy/VLAN.

## Limitations

- MetalLB is installed automatically if missing (user-managed per plan is not enforced; apply will succeed if already present).
- Service selector is fixed to `ingresscontroller.operator.openshift.io/deployment-ingresscontroller: default` — override not yet supported.
- Dual-stack not tested (IPv4 only).
- No validating webhook; required fields validated at runtime (Degraded if missing).
