# Historical enhancement: hosted-cluster apps ingress

**PR:** PR-7
**Status:** Historical record
**Last updated:** 2026-08-25

This document preserves the original apps-ingress enhancement checklist. The
current API and workflow are attachment-scoped; use
[Apps ingress for hosted clusters](../apps-ingress.md) and the
[website guide](../../website/docs/guides/apps-ingress.md) as the authority.

## Original goal

Add optional MetalLB-based `*.apps.<cluster>.<baseDomain>` routing for a
HostedCluster running on an isolated VLAN. The shared `Infra` should aggregate
the resulting VIP into split-horizon DNS and Envoy, while the
`InfraClusterAttachment` should own the hosted-cluster workflow.

## Current implementation

- `InfraClusterAttachment.spec.appsIngress` contains the MetalLB pool, hosted
  Service, metadata, and HTTP/HTTPS port settings.
- The attachment controller uses the HostedCluster kubeconfig, waits for a Ready
  worker, installs/configures MetalLB, and manages the hosted `oooi-ingress`
  Service.
- The attachment status records `Pending`, `Ready`, or `Degraded` state,
  endpoint, reason, message, and applied-resource identities used for cleanup.
- The Infra controller adds Envoy backends after an IP or hostname endpoint is
  available; wildcard A records require an external IP.
- The attachment finalizer deletes the configured hosted resources by name and
  the cross-namespace control-plane NetworkPolicy; OLM leftovers and public DNS
  remain manual cleanup.
- Public DNS remains the responsibility of ExternalDNS or another DNS writer.
- Shared Infra resources are not owned by attachments; `Infra` remains the only
  writer of the shared DHCP, DNS, and proxy children.

## Related current behavior

The shared proxy also supports source-scoped Kubernetes aliases for KubeVirt
workers. NodePool membership and CAPI Machine status identify worker source
addresses; addresses outside the Infra CIDR are ignored. See the
[multi-cluster guide](../../website/docs/guides/multi-cluster.md).

## Historical questions resolved

- External endpoint discovery is performed from the hosted Service status and
  supports both IP and hostname endpoints. Hostname-only endpoints do not
  produce oooi-generated wildcard A records.
- DNS and proxy wildcard generation is part of shared Infra aggregation.
- Pending and degraded reconciliation uses explicit status reasons and
  requeues.
- MetalLB installation is automatic when apps ingress is enabled.
- The default IngressController selector is fixed and the current flow is IPv4
  and L2-oriented; validate BGP and dual-stack separately.

Older checklist entries below are retained only as history and are not pending
work items:

- External IP discovery
- Wildcard DNS and proxy integration
- Pending/ready transitions
- Full E2E coverage
- Public-DNS documentation
