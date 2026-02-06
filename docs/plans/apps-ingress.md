# Plan: Hosted Cluster *.apps Ingress Handling

## Summary
Implement optional, user-managed MetalLB integration for hosted clusters and configure wildcard `*.apps.<cluster>.<tld>` resolution across VLAN and pod networks. The operator will create a dedicated LoadBalancer Service in the hosted cluster and route traffic through the existing proxy. DNS will be split-horizon: VLAN-side resolves to MetalLB external IP; pod-network-side resolves to the proxy Service IP. TLS remains passthrough.

## Goals
- Provide automated configuration for `*.apps.<cluster>.<tld>` based on hosted cluster lifecycle.
- Ensure wildcard app traffic is routable from:
  - VLAN network (via MetalLB external IP).
  - Pod network (via internal proxy Service IP).
- Ensure `api.<cluster>.<tld>` is resolvable on the pod network using the DNS setup.
- Do not require operator-managed MetalLB installation.
- Degrade gracefully if hosted cluster is unavailable or external IP not assigned yet.

## Non-goals
- Terminate TLS in the proxy (TLS passthrough only).
- Auto-discover ingress endpoints from Route/Ingress objects (we will create a dedicated Service instead).
- Manage or install MetalLB operator components (user-managed only).

## Assumptions and Constraints
- Hosted cluster provides kubeconfig in the secret referenced by `HostedCluster.Status.KubeConfig`.
- MetalLB is already installed and configured by the user in the hosted cluster.
- External IP is assigned by MetalLB to the dedicated LoadBalancer Service.
- The operator manages infrastructure components on the management cluster only.

## API Changes
Add `AppsIngressConfig` to the Infra API spec to declare optional apps handling and MetalLB scope.

### Proposed fields (InfraSpec)
- `appsIngress.enabled` (bool, default false): Enable apps ingress automation.
- `appsIngress.baseDomain` (string): Base domain for apps (e.g., `apps.<cluster>.<tld>` prefix is derived).
- `appsIngress.hostedClusterRef`:
  - `name` (string)
  - `namespace` (string, default `clusters`)
- `appsIngress.metallb` (object): User-managed MetalLB scope and external IP expectations.
  - `addressPoolName` (string): Address pool name for annotations.
  - `ipAddressPoolRange` (string or array): Optional for validation only (no enforcement).
  - `l2AdvertisementName` (string, optional)
- `appsIngress.service` (object): Service name and namespace in hosted cluster.
  - `name` (string, default `<cluster>-apps`)
  - `namespace` (string, default `clusters-<cluster>` or hosted cluster namespace)
- `appsIngress.ports` (object):
  - `http` (int, default 80)
  - `https` (int, default 443)

### Status additions (InfraStatus)
- `appsIngress` status block:
  - `phase` (Pending/Ready/Degraded)
  - `externalIP` (string)
  - `lastSyncTime` (timestamp)
  - `reason/message`

## Controller Workflow

### 1) InfraReconciler orchestration
- When `appsIngress.enabled` is true:
  - Fetch hosted cluster kubeconfig secret.
  - Build client for hosted cluster.
  - Create/patch LoadBalancer Service for ingress (hosted cluster).
  - Discover external IP assigned by MetalLB.
  - Update DNS and proxy component specs accordingly.

### 2) Hosted Cluster kubeconfig access
- Read HostedCluster via management cluster client.
- Use `HostedCluster.Status.KubeConfig.Name` to get secret data (`kubeconfig` key).
- Create controller-runtime client for hosted cluster.
- Degrade with retry if kubeconfig is missing or access fails.

### 3) Dedicated LoadBalancer Service
- Create Service in hosted cluster targeting the ingress controller (router NodePorts).
- Selector should target ingress controller pods or service.
- Annotate with MetalLB address pool if configured.
- Ports 80/443 with `type: LoadBalancer`.
- Persist Service name for DNS and proxy mapping.

### 4) External IP discovery
- Read `.status.loadBalancer.ingress[0].ip` from the hosted cluster Service.
- If empty, mark apps ingress status as Pending and requeue.
- When populated, store in InfraStatus.appsIngress.externalIP.

### 5) DNS split-horizon updates
- Extend DNS CRD generation to include wildcard:
  - `*.apps.<cluster>.<tld>` -> MetalLB external IP (VLAN view).
  - `*.apps.<cluster>.<tld>` -> proxy service IP (pod-network view).
- Ensure `api.<cluster>.<tld>` resolves on pod network to proxy IP.

### 6) Proxy updates
- Add proxy backend for apps wildcard:
  - Listener on 80/443
  - SNI wildcard routing for `*.apps.<cluster>.<tld>`
  - Backend target is hosted cluster LB external IP
  - TCP passthrough; no TLS termination
- Allow connection to external IP over VLAN from proxy pod via policy-based routing.

### 7) Degradation handling
- If hosted cluster is unavailable, MetalLB IP missing, or LB Service creation fails:
  - Set appsIngress status to Degraded with reason/message.
  - Keep existing proxy and DNS config unchanged.
  - Requeue with backoff.

## RBAC and Permissions
- Add RBAC for HostedCluster read access (management cluster).
- No permissions required inside hosted cluster (access is via kubeconfig).
- Ensure no new RBAC privileges are granted for MetalLB resources in management cluster.

## Testing Strategy

### Unit tests (envtest)
- Test that when `appsIngress.enabled` is set:
  - InfraReconciler attempts to read hosted cluster kubeconfig.
  - DNS config includes wildcard records (both views).
  - Proxy config includes SNI wildcard listeners.
  - Degraded status when external IP is missing.

### Integration tests (optional)
- Simulate hosted cluster Service with external IP via fake client.
- Verify status transitions Pending -> Ready.

### E2E tests (kind)
- Create a hosted cluster with MetalLB preinstalled.
- Verify Service creation, external IP assignment, and DNS resolution.
- Verify traffic to `*.apps` resolves on VLAN and pod network.

## Rollout Plan
1. Add API fields and regenerate CRDs.
2. Implement controller logic behind `appsIngress.enabled`.
3. Update DNS and proxy generation.
4. Add tests.
5. Document usage in existing setup docs (no new plan doc beyond this).

## Risks and Mitigations
- **Hosted cluster access fails**: Degrade and retry with backoff.
- **MetalLB not installed**: Degrade with clear message.
- **External IP not assigned**: Pending status and retry.
- **Wildcard DNS misconfiguration**: Validate base domain format in API.

## Open Questions
- Should Service live in `clusters-<name>` or hosted cluster namespace?
- How should selector target ingress controller pods in hosted cluster?
- Should we support dual-stack external IPs for `*.apps`?

## Acceptance Criteria
- `*.apps.<cluster>.<tld>` resolves from VLAN to MetalLB external IP.
- `*.apps.<cluster>.<tld>` resolves from pod network to proxy service IP.
- `api.<cluster>.<tld>` resolves from pod network to proxy service IP.
- Traffic to `*.apps` is routed through proxy to hosted cluster ingress LB.
- Degraded status is set when MetalLB IP is not available.
