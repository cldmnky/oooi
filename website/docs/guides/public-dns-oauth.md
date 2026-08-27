# Public DNS and OAuth publishing

oooi manages **VLAN-side** DNS itself but deliberately does **not** write
public DNS records. This guide explains who can publish what, and gives two
supported patterns: publishing the API and `*.apps` records, and putting the
**OAuth** endpoint on a MetalLB VIP when HyperShift forces the `Route` strategy.

## Ownership matrix

| Record | Created by | Watched Service | Where that Service lives |
|---|---|---|---|
| `api.<cluster>.<domain>` (public API VIP) | Your ExternalDNS | `kube-apiserver` | **Hosting** (management) cluster, `clusters-<name>` control-plane namespace |
| `*.apps.<cluster>.<domain>` | Your ExternalDNS | `<service.name>` (`oooi-ingress`) | **Hosted** cluster `openshift-ingress` |
| `oauth.<cluster>.<domain>` (public VIP) | Your ExternalDNS | `<attachment>-proxy-external` | **Hosting** (management) cluster, Infra namespace |
| VLAN views of these records | **oooi** (automatic) | — | DNSServer static zones |

Key constraint: an ExternalDNS instance can only see Services in the cluster
its kubeconfig points at. A default management-cluster ExternalDNS **cannot**
see the hosted cluster's `oooi-ingress` Service. Conversely, the hosted-cluster
watcher cannot see the management-side `kube-apiserver` Service in the
`clusters-<name>` control-plane namespace. Ensure the management ExternalDNS
instance watches that Service too; if it uses a publish label filter, add the
matching label to each API Service.

## Pattern A — publish API and `*.apps` records

The HostedCluster creates a management-cluster `kube-apiserver` LoadBalancer
Service in its control-plane namespace. Run a management-cluster ExternalDNS
instance that watches this Service and publishes
`api.<cluster>.<domain>` from its hostname annotation. If that instance uses a
label filter, apply its matching label to each API Service:

```bash
kubectl -n clusters-example-hcp get service kube-apiserver -o wide
kubectl -n clusters-example-hcp label service kube-apiserver \
  external-dns.example.com/publish=yes --overwrite
```

The API Service VIP is independent from the per-attachment OAuth VIP. Verify
the public API record and endpoint after the management ExternalDNS sync:

```bash
dig +short api.example-hcp.clusters.example.com @<public-resolver>
curl -k -o /dev/null -w '%{http_code}\n' \
  https://api.example-hcp.clusters.example.com:6443/version
# 200
```

Run a separate ExternalDNS instance that can watch the hosted cluster to
publish the annotated apps-ingress Service. Two supported deployment variants
are:

=== "ExternalDNS Operator inside the hosted cluster"

    Configure the Red Hat External DNS Operator `ExternalDNS` resource with
    provider credentials and a label filter such as
    `external-dns.example.com/publish=yes`. Then make sure the
    `InfraClusterAttachment` carries matching metadata. Confirm that the Operator version and supported
    providers match your OpenShift Container Platform release.

    ```yaml
    spec:
      appsIngress:
        service:
          annotations:
            external-dns.alpha.kubernetes.io/hostname: "*.apps.mycluster.example.com."
          labels:
            external-dns.example.com/publish: "yes"
    ```

=== "Dedicated Deployment with a hosted-cluster kubeconfig"

    Run ExternalDNS anywhere, pointed at the hosted API through a kubeconfig
    secret. The following arguments are a generic Route53 example:

    ```yaml
    args:
      - --kubeconfig=/etc/kubeconfig/config          # hosted-cluster kubeconfig
      - --provider=aws
      - --source=service
      - --policy=sync                                # also removes stale records
      - --registry=txt
      - --txt-owner-id=example-hcp-external-dns
      - --txt-prefix=external-dns-
      - --zone-id-filter=<ROUTE53_HOSTED_ZONE_ID>
      - --label-filter=external-dns.example.com/publish=yes
      - --service-type-filter=LoadBalancer
    ```

    Build the hosted-cluster kubeconfig so it resolves internally (avoids
    depending on public DNS during bootstrap):

    ```bash
    tmp=$(mktemp -d)
    kubectl -n clusters get secret example-hcp-admin-kubeconfig \
      -o jsonpath='{.data.kubeconfig}' | base64 -d > "$tmp/config"
    sed -i.bak \
      's#server: https://api.example-hcp.clusters.example.com:6443#server: https://kube-apiserver.clusters-example-hcp.svc.cluster.local:6443\n    tls-server-name: api.example-hcp.clusters.example.com#' \
      "$tmp/config"
    rm -f "$tmp/config.bak"
    kubectl -n external-dns-operator create secret generic \
      example-hcp-external-dns-kubeconfig \
      --from-file=config="$tmp/config" --dry-run=client -o yaml | kubectl apply -f -
    rm -rf "$tmp"
    ```

    !!! warning

        Prefer a read-only hosted-cluster ServiceAccount kubeconfig instead of
        the admin kubeconfig in shared environments.

## Pattern B — publish OAuth via a per-cluster hosting VIP

For the KubeVirt hosted-control-plane configuration used by this guide, OAuth
uses the `Route` publishing strategy. Because `spec.services` is immutable, plan
the publishing strategy before creating the HostedCluster. Enable one external
Service on each attachment that needs public OAuth:

```mermaid
flowchart LR
    B["Browser / clients<br/>(anywhere)"] -->|"https://oauth.<cluster>.<domain>"| DNS["Public DNS"]
    DNS -->|"A → VIP"| LB["MetalLB VIP<br/>(hosting cluster)"]
    LB --> SVC["Service &lt;attachment&gt;-proxy-external<br/>LoadBalancer :443 only"]
    SVC --> E["Envoy pod"]
    E -->|"SNI passthrough"| O["Hosted OAuth server<br/>ClusterIP :443"]
```

1. Enable the external Service on the attachment — it selects the shared Envoy
   pod but exposes **only** the configured ingress port (443). Envoy's admin
   port `9901` and backend ports stay ClusterIP-only:

   ```yaml
   spec:
     externalService:
       enabled: true
       addressPoolName: hosting-public-pool                # hosting-cluster IPAddressPool
       annotations:
         external-dns.alpha.kubernetes.io/hostname: oauth.example-hcp.clusters.example.com.
       labels:
         external-dns.example.com/publish: "yes"
   ```

2. Ensure the HostedCluster uses the default `Route` OAuth publishing strategy
   (KubeVirt default) so the OAuth Route exists under the cluster domain.
3. Let any ExternalDNS that watches the **hosting cluster** pick up the new
   LoadBalancer Service — it lives in your Infra namespace, so no special
   kubeconfig is needed:

   ```bash
   kubectl -n clusters get svc <attachment>-proxy-external -o wide
   # TYPE           EXTERNAL-IP     PORT(S)
   # LoadBalancer   198.51.100.20     443:<node-port>/TCP
   ```

4. Verify convergence:

   ```bash
   dig +short oauth.example-hcp.clusters.example.com @<public-resolver>
   curl -k -o /dev/null -w '%{http_code}\n' \
     'https://oauth.example-hcp.clusters.example.com/oauth/authorize?client_id=openshift-challenging-client&response_type=token'
   # → 401 (unauthenticated reach = healthy path)
   ```

Because the record is derived from Service status, **VIP changes follow
automatically** — no stale records after pool changes or re-allocation.

## Why this design

| Alternative | Rejected because |
|---|---|
| Changing OAuth publishing after creation | `spec.services` on the HostedCluster is immutable; create the HostedCluster with the required strategy |
| Publishing via hosting-cluster router Routes | Exposes tenant traffic on the hosting cluster's ingress infrastructure — breaks isolation goals |
| Manual A records | Stale after VIP changes; silently breaks console/canary route health |

## Cleanup

Records are removed by their owners:

- Disabling `spec.externalService.enabled` on an attachment (or deleting the
  attachment or Infra) removes that cluster's Service; a `--policy=sync`
  ExternalDNS deletes its records on the next sync.
- Deleting a HostedCluster removes its management-side API Service; a
  management-cluster ExternalDNS running with `--policy=sync` deletes the API
  record on the next sync.
- Deleting the hosted-cluster ExternalDNS instance leaves its TXT/A pairs
  orphaned — delete the Deployment **and** remove leftover records manually if
  the zone must be clean. See [Uninstall and cleanup](../operations/uninstall.md).
