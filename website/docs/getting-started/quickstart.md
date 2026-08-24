# Quickstart

This walkthrough creates a KubeVirt hosted cluster, attaches its workers to an
isolated VLAN, and adds oooi infrastructure for DHCP, DNS, the control-plane
proxy, apps ingress, and public OAuth DNS.

The names, addresses, registry, and zone below are documentation values. Replace
them before applying anything.

## Learning objectives

After completing this quickstart, you can:

- deploy the oooi controller on an OpenShift Container Platform management cluster;
- configure an `Infra` resource for an isolated worker VLAN;
- verify control-plane, applications, and public-DNS connectivity; and
- identify the status fields and logs to inspect when reconciliation does not complete.

## Before you begin

Complete the [prerequisites](prerequisites.md), including the
`NetworkAttachmentDefinition`, hosted-cluster secrets, IP plan, and an image
registry that your cluster can pull from. The examples target OpenShift Container
Platform 4.22. Replace the example release image with a payload supported by
your HyperShift and Red Hat OpenShift Virtualization versions.

## 1. Install oooi

Install the CRDs and the controller in `oooi-system`:

```bash
export OOOI_IMAGE=registry.example.com/oooi:latest

make install
make deploy IMG="$OOOI_IMAGE"
kubectl -n oooi-system rollout status deployment/oooi-controller-manager --timeout=5m
```

## 2. Create the HostedCluster

The example follows the structure used by a KubeVirt HostedCluster and NodePool:

- `spec.services` is a list of service publishing strategies.
- KubeVirt worker networks are configured with `additionalNetworks`.
- `attachDefaultNetwork: false` leaves workers with only the VLAN interface.

The service publishing strategy is immutable after creation. On KubeVirt, OAuth
uses a `Route`; [publish it through the proxy external Service](../guides/public-dns-oauth.md)
when it must be reachable from outside the VLAN.

```yaml title="hosted-example-hcp.yaml"
apiVersion: hypershift.openshift.io/v1beta1
kind: HostedCluster
metadata:
  name: example-hcp
  namespace: clusters
spec:
  release:
    image: registry.example.com/openshift-release:4.22.10
  dns:
    baseDomain: clusters.example.com
  networking:
    clusterNetwork:
      - cidr: 10.132.0.0/14
    serviceNetwork:
      - cidr: 172.31.0.0/16
    networkType: OVNKubernetes
  platform:
    type: KubeVirt
    kubevirt: {}
  services:
    - service: APIServer
      servicePublishingStrategy:
        type: LoadBalancer
        loadBalancer:
          hostname: api.example-hcp.clusters.example.com
    - service: OAuthServer
      servicePublishingStrategy:
        type: Route
        route:
          hostname: oauth.example-hcp.clusters.example.com
    - service: Konnectivity
      servicePublishingStrategy:
        type: Route
        route:
          hostname: konnectivity.example-hcp.clusters.example.com
    - service: Ignition
      servicePublishingStrategy:
        type: Route
        route:
          hostname: ignition.example-hcp.clusters.example.com
  pullSecret:
    name: pull-secret
  sshKey:
    name: ssh-key
  secretEncryption:
    type: aescbc
    aescbc:
      activeKey:
        name: etcd-encryption-key
---
apiVersion: hypershift.openshift.io/v1beta1
kind: NodePool
metadata:
  name: example-hcp
  namespace: clusters
spec:
  arch: amd64
  clusterName: example-hcp
  replicas: 3
  management:
    autoRepair: true
    upgradeType: Replace
  platform:
    type: KubeVirt
    kubevirt:
      additionalNetworks:
        - name: default/vlan100
      attachDefaultNetwork: false
      compute:
        cores: 4
        memory: 16Gi
      rootVolume:
        type: Persistent
        persistent:
          size: 50Gi
  release:
    image: registry.example.com/openshift-release:4.22.10
```

```bash
kubectl apply -f hosted-example-hcp.yaml
kubectl -n clusters get hostedcluster,nodepool
```

## 3. Apply the Infra resource

Apply `Infra` as soon as the HostedCluster object exists. The DHCP, DNS, and
proxy services are needed while workers bootstrap; do not wait for the hosted
cluster to become Available.

```yaml title="infra-example-hcp.yaml"
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: Infra
metadata:
  name: example-hcp
  namespace: clusters
spec:
  networkConfig:
    cidr: 192.0.2.0/24
    gateway: 192.0.2.1
    networkAttachmentDefinition: vlan100
    networkAttachmentNamespace: default
    dnsServers:
      - 198.51.100.53
      - 198.51.100.54
  infraComponents:
    dhcp:
      serverIP: 192.0.2.2
      rangeStart: 192.0.2.100
      rangeEnd: 192.0.2.199
      leaseTime: 1h
    dns:
      serverIP: 192.0.2.3
      clusterName: example-hcp
      baseDomain: clusters.example.com
    proxy:
      serverIP: 192.0.2.4
      controlPlaneNamespace: clusters-example-hcp
      internalProxyService: example-hcp-proxy.clusters.svc.cluster.local
      externalService:
        enabled: true
        addressPoolName: hosting-public-pool
        annotations:
          external-dns.alpha.kubernetes.io/hostname: oauth.example-hcp.clusters.example.com.
        labels:
          external-dns.example.com/publish: "yes"
  appsIngress:
    enabled: true
    hostedClusterRef:
      name: example-hcp
      namespace: clusters
    metallb:
      addressPoolName: apps-pool
      ipAddressPoolRange: 192.0.2.200-192.0.2.220
    service:
      annotations:
        external-dns.alpha.kubernetes.io/hostname: "*.apps.example-hcp.clusters.example.com."
      labels:
        external-dns.example.com/publish: "yes"
```

```bash
kubectl apply -f infra-example-hcp.yaml
```

## 4. Watch it converge

```bash
kubectl -n clusters wait --for=condition=Available \
  hostedcluster/example-hcp --timeout=30m
kubectl -n clusters wait --for=condition=Ready \
  infra/example-hcp --timeout=30m

kubectl -n clusters get dhcpserver,dnsserver,proxyserver,infra
kubectl -n clusters get infra example-hcp \
  -o jsonpath='{.status.appsIngressStatus.phase}{" "}{.status.appsIngressStatus.reason}{" ip="}{.status.appsIngressStatus.externalIP}{"\n"}'
```

Apps ingress normally progresses through `WaitingForHostedClusterNodes`,
`WaitingForMetalLBCRDs`, `WaitingForExternalIP`, and `Ready`.

If either wait command times out, inspect the `Ready` condition and the
apps-ingress status before retrying. The [troubleshooting guide](../operations/troubleshooting.md)
maps the reported reason to corrective actions.

## 5. Verify from the VLAN

Run these from a client attached to the VLAN, not from a management-cluster pod:

```bash
dig @192.0.2.3 +short api.example-hcp.clusters.example.com
# 192.0.2.4
dig @192.0.2.3 +short console-openshift-console.apps.example-hcp.clusters.example.com
# 192.0.2.200

curl -k https://api.example-hcp.clusters.example.com:6443/version
curl -k -o /dev/null -w '%{http_code}\n' \
  'https://oauth.example-hcp.clusters.example.com/oauth/authorize?client_id=openshift-challenging-client&response_type=token'
curl -k -o /dev/null -w '%{http_code}\n' \
  https://console-openshift-console.apps.example-hcp.clusters.example.com
```

| Check | Expected result |
|---|---|
| API `/version` | `200` |
| Unauthenticated OAuth request | `401` |
| Console route | `200` |
| Ignition root path | `404` |
| Plain HTTP to konnectivity | `415` |

The OAuth `401`, Ignition `404`, and konnectivity `415` responses show that the
SNI proxy reached the intended backend. From the pod network, the same HCP
names should resolve to the proxy ClusterIP rather than the VLAN addresses.

## 6. Publish public DNS

Use ExternalDNS or your DNS provider to publish `*.apps` and OAuth. See
[Public DNS and OAuth publishing](../guides/public-dns-oauth.md) for the
ownership model and a hosted-cluster ExternalDNS pattern.

```bash
dig +short console-openshift-console.apps.example-hcp.clusters.example.com @<public-resolver>
dig +short oauth.example-hcp.clusters.example.com @<public-resolver>
```

Both answers should match their current MetalLB VIPs. Use the
[verification guide](../operations/verify.md) for the expected DNS and HTTP
results.
