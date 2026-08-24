# Quickstart

This walkthrough takes you from an installed operator to a verified hosted
cluster with apps ingress and a publicly resolvable OAuth endpoint. It mirrors
the validated lab configuration shipped in
[`config/samples/species-8472-infra.yaml`](https://github.com/cldmnky/oooi/blob/main/config/samples/species-8472-infra.yaml).

**Estimated time:** 45–60 minutes (mostly the hosted cluster bootstrap).

## 0. Complete the prerequisites

Make sure you have verified every item in [Prerequisites](prerequisites.md) —
in particular the NAD, the three HCP secrets, and your IP plan.

## 1. Install oooi

Install CRDs and the controller into namespace `oooi-system`:

```bash
# Use the published multi-arch image, or build your own (see Installation)
export OOOI_IMAGE=quay.io/cldmnky/oooi:latest

make install
make deploy IMG="$OOOI_IMAGE"
kubectl -n oooi-system rollout status deployment/oooi-controller-manager --timeout=5m
```

## 2. Create the HostedCluster

Create a KubeVirt-hosted cluster whose workers live on the isolated VLAN. The
essential properties are `attachDefaultNetwork: false` on the NodePool platform
and matching `dns` / service publishing settings. Example skeleton:

```yaml
apiVersion: hypershift.openshift.io/v1beta1
kind: HostedCluster
metadata:
  name: species-8472
  namespace: clusters
spec:
  release:
    image: quay.io/openshift-release-dev/ocp-release:4.22.10-multi
  dns:
    baseDomain: clusters.blahonga.me        # api.<name>.<baseDomain> etc.
  networking:
    clusterNetwork:
      - cidr: 10.132.0.0/14
    serviceNetwork:
      - cidr: 172.31.0.0/16
  platform:
    type: KubeVirt
    kubevirt:
      baseDomainPolicies:
        - ExternalDNS                        # publish via Routes, not LBs
      controlPlaneServiceType: ClusterIP
  services:
    # Immutable after creation — choose deliberately.
    servicePublishingStrategy:
      apiserver:
        type: Route                          # SNI-proxied through oooi Envoy
      oauthServer:
        type: Route                          # published via proxy external Service
      ignition:
        type: Route
      konnectivity:
        type: Route
  pullSecret:
    name: pullsecret-clusters
  sshKey:
    name: sshkey-clusters
  etcdEncryptionKeySecretRef:
    name: etcd-encryption-key-clusters
---
apiVersion: hypershift.openshift.io/v1beta1
kind: NodePool
metadata:
  name: species-8472
  namespace: clusters
spec:
  clusterName: species-8472
  replicas: 3
  management:
    autoRepair: true
  platform:
    type: KubeVirt
    kubevirt:
      attachDefaultNetwork: false            # workers only see the VLAN
      nodeNetworking:
        network:
          networkAttachmentDefinition:
            name: vlan203
            namespace: default
```

Apply it and wait for the object to be accepted (not Available — that comes later):

```bash
kubectl apply -f hosted-species-8472.yaml
kubectl -n clusters get hostedcluster,nodepool
```

!!! important "Publishing strategy is immutable"

    HyperShift rejects changes to `spec.services` after creation. On KubeVirt,
    OAuth defaults to `Route`; a direct `LoadBalancer` OAuth service is **only**
    supported on self-managed Azure. To put OAuth on a MetalLB VIP anyway, use
    [Public DNS and OAuth publishing](../guides/public-dns-oauth.md).

## 3. Apply the Infra resource

Declare the VLAN infrastructure immediately after the HostedCluster exists:

```yaml title="infra-species-8472.yaml"
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: Infra
metadata:
  name: species-8472
  namespace: clusters
spec:
  networkConfig:
    cidr: 10.202.64.0/24
    gateway: 10.202.64.1
    networkAttachmentDefinition: vlan203
    networkAttachmentNamespace: default
    dnsServers:
      - 10.201.0.2          # upstream resolvers for non-cluster queries
      - 10.201.0.1
  infraComponents:
    dhcp:
      enabled: true
      serverIP: 10.202.64.2
      rangeStart: 10.202.64.200
      rangeEnd: 10.202.64.254
      leaseTime: 1h
    dns:
      enabled: true
      serverIP: 10.202.64.3
      clusterName: species-8472
      baseDomain: clusters.blahonga.me
    proxy:
      enabled: true
      serverIP: 10.202.64.4
      controlPlaneNamespace: clusters-species-8472
      internalProxyService: species-8472-proxy.clusters.svc.cluster.local
      # Publish the OAuth endpoint through the hosting-cluster MetalLB so
      # ExternalDNS can map it to a VIP. See the Public DNS guide.
      externalService:
        enabled: true
        addressPoolName: metallb-pool
        annotations:
          external-dns.alpha.kubernetes.io/hostname: oauth.species-8472.clusters.blahonga.me.
        labels:
          external-dns.blahonga.me/publish: "yes"
  appsIngress:
    enabled: true
    hostedClusterRef:
      name: species-8472
      namespace: clusters
    metallb:
      addressPoolName: vlan203-apps
      ipAddressPoolRange: 10.202.64.180-10.202.64.190
    service:
      annotations:
        external-dns.alpha.kubernetes.io/hostname: "*.apps.species-8472.clusters.blahonga.me."
      labels:
        external-dns.blahonga.me/publish: "yes"
    ports:
      http: 80
      https: 443
```

```bash
kubectl apply -f infra-species-8472.yaml
```

## 4. Watch it converge

```bash
# Hosted control plane becomes available (~15 min)
kubectl -n clusters wait --for=condition=Available \
  hostedcluster/species-8472 --timeout=30m

# Infrastructure becomes Ready
kubectl -n clusters wait --for=condition=Ready \
  infra/species-8472 --timeout=30m

kubectl -n clusters get dhcpserver,dnsserver,proxyserver,infra
```

Apps-ingress progress is reported separately:

```bash
kubectl -n clusters get infra species-8472 \
  -o jsonpath='{.status.appsIngressStatus.phase}{" "}{.status.appsIngressStatus.reason}{" ip="}{.status.appsIngressStatus.externalIP}{"\n"}'
```

Typical progression: `WaitingForHostedClusterNodes` → `WaitingForMetalLBCRDs`
→ `WaitingForExternalIP` → `Ready`.

## 5. Verify from the VLAN

Run these from a real VLAN client (or a VLAN-attached probe), not just a
management pod:

```bash
dig @10.202.64.3 +short api.species-8472.clusters.blahonga.me                 # → 10.202.64.4
dig @10.202.64.3 +short console-openshift-console.apps.species-8472.clusters.blahonga.me  # → 10.202.64.180

curl -k https://api.species-8472.clusters.blahonga.me:6443/version            # HTTP 200
curl -k -o /dev/null -w '%{http_code}\n' \
  'https://oauth.species-8472.clusters.blahonga.me/oauth/authorize?client_id=openshift-challenging-client&response_type=token'   # HTTP 401 (expected)
curl -k -o /dev/null -w '%{http_code}\n' \
  https://console-openshift-console.apps.species-8472.clusters.blahonga.me    # HTTP 200
```

| Check | Expected |
|---|---|
| API `/version` | `200` |
| Unauthenticated OAuth | `401` (proves TLS reached the OAuth backend) |
| Console | `200` |
| Ignition `/` | `404` (normal at root path) |
| konnectivity plain HTTP | `415` |

From the **pod network**, the same names must resolve to the proxy *ClusterIP*
instead of VLAN IPs — that is the split-horizon view working.

## 6. Publish public DNS

If clients outside the VLAN need to reach OAuth or `*.apps`, run an ExternalDNS
instance that can watch the relevant Services and write records to your public
zone. Full instructions, including why a management-cluster ExternalDNS cannot
see these Services, are in
[Public DNS and OAuth publishing](../guides/public-dns-oauth.md).

Quick check once converged (`1.1.1.1` = any public resolver):

```bash
dig +short console-openshift-console.apps.species-8472.clusters.blahonga.me @1.1.1.1
dig +short oauth.species-8472.clusters.blahonga.me @1.1.1.1
```

Both should return the current MetalLB-assigned VIPs.

## Next steps

- Understand what was created: [Architecture](../architecture.md)
- Tune DNS behavior: [Split-horizon DNS](../guides/split-horizon-dns.md)
- Learn the apps-ingress state machine: [Apps ingress](../guides/apps-ingress.md)
