# Apps Ingress Design

## Summary

`InfraClusterAttachment` owns the per-hosted-cluster apps-ingress workflow.
The shared `Infra` owns the VLAN services and aggregates each attachment's
ready endpoint into the common DNS and Envoy configuration. TLS remains
passthrough.

## Goals

- Provide optional `*.apps.<cluster>.<domain>` routing for each attachment.
- Make the wildcard reachable from the VLAN through the MetalLB VIP.
- Make the wildcard reachable from the management network through the shared
  proxy Service.
- Degrade gracefully while the HostedCluster, workers, MetalLB, or VIP is not
  ready.
- Keep shared DHCP, DNS, and proxy child resources owned and reconciled by
  `Infra` only.

## API

`Infra` contains only shared network and component settings. Each hosted
cluster requires an `InfraClusterAttachment` in the same namespace as its
referenced `Infra`:

- `spec.infraRef.name`: shared VLAN infrastructure.
- `spec.hostedClusterRef`: HyperShift HostedCluster used for hosted-cluster
  API access.
- `spec.dns.clusterName` and `spec.dns.baseDomain`: names used for DNS and SNI.
- `spec.controlPlaneNamespace`: optional management-cluster control-plane
  namespace; defaults to `<hostedClusterRef.namespace>-<name>`.
- `spec.appsIngress`: optional MetalLB pool, hosted Service, and port settings.

The attachment status reports the apps-ingress phase, endpoint, reason, and
message. The parent Infra reports aggregate attachment counts.

## Controller Workflow

### Attachment reconciliation

1. Resolve the referenced `Infra` and derive control-plane defaults.
2. Ensure the control-plane `allow-infrastructure` NetworkPolicy once its
   namespace exists.
3. When apps ingress is enabled, wait for a Ready hosted worker.
4. Read the HostedCluster kubeconfig and connect to the hosted API through its
   in-cluster API Service.
5. Ensure the MetalLB Subscription, `MetalLB`, `IPAddressPool`, and
   `L2Advertisement` resources.
6. Ensure the hosted `LoadBalancer` Service for the default ingress controller.
7. Store the assigned IP or hostname in attachment status and requeue while it
   is pending.

### Shared aggregation

On every attachment event, the Infra controller lists active attachments and
rebuilds the shared child specs deterministically. Each valid attachment adds
the five fully qualified HCP names and, when its apps status is Ready, the
wildcard apps names. KubeVirt attachments with CAPI Machine addresses inside
the Infra CIDR also add a source-scoped backend for `kubernetes`,
`kubernetes.default`, `kubernetes.default.svc`, and
`kubernetes.default.svc.cluster.local`. Duplicate HostedCluster references,
duplicate domains, and duplicate source IPs are reported instead of silently
resolved; only the conflicting routes are excluded.

There is no implicit single-cluster binding. Removing an attachment removes its
records and backends on the next Infra reconciliation. Alias discovery watches
HyperShift NodePools for membership and CAPI Machines for address changes; it
does not require a remote VMI informer.

### Cleanup

The attachment finalizer removes hosted apps-ingress resources and the
cross-namespace control-plane NetworkPolicy before allowing deletion. If the
hosted API or its CRDs are already gone, cleanup treats those resources as
absent and still removes the finalizer.

## Testing

- Envtest covers deterministic multi-attachment aggregation, conflict
  exclusion, status summaries, policy creation, and cleanup.
- Apps-ingress unit tests cover waiting for a Ready hosted node and avoiding
  cleanup when apps ingress was never enabled.
- E2E covers shared Infra resources, attachment-driven DNS and SNI routing,
  attachment cleanup, and the optional apps-ingress path where a HyperShift
  HostedCluster is available.
