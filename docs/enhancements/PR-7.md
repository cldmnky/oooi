# Enhancement Status: Hosted Cluster Apps Ingress Support

**PR Number:** PR-7
**Status:** In Progress
**Last Updated:** 2026-02-06

> Historical record. The implementation now scopes apps-ingress configuration
> and status to `InfraClusterAttachment`; see [Apps Ingress](../apps-ingress.md)
> for the current API and workflow.

## Summary
This PR introduces optional MetalLB-based ingress configuration for hosted clusters to enable `*.apps.<cluster>.<tld>` wildcard routing. The implementation adds API types, status tracking, and controller logic to automatically provision MetalLB infrastructure and LoadBalancer services in hosted clusters, with split-horizon DNS support for VLAN and pod network access.

## Implementation Status

### Completed Features
- [x] **API Schema**: Added `AppsIngressConfig` struct with comprehensive configuration options (MetalLB pool, service config, ports)
- [x] **Status Tracking**: Implemented `AppsIngressStatus` to track phase (Pending/Ready/Degraded), external IP, sync time, and status messages
- [x] **CRD Generation**: Updated CRD YAML with new `appsIngress` and `appsIngressStatus` fields including validation and descriptions
- [x] **Deep Copy Methods**: Generated deepcopy methods for all new types for Kubernetes compatibility
- [x] **RBAC Updates**: Extended RBAC rules to grant access to `secrets` and `hostedclusters` resources
- [x] **Hosted Cluster Client**: Implemented kubeconfig-based client creation to access hosted cluster API
- [x] **MetalLB Installation**: Controller logic to install MetalLB operator via Subscription and configure IPAddressPool + L2Advertisement
- [x] **LoadBalancer Service**: Creates/updates LoadBalancer Service targeting OpenShift ingress controller pods with MetalLB annotations
- [x] **Reconciliation Orchestration**: Main reconciliation flow calling apps ingress logic when enabled
- [x] **Degradation Handling**: Sets degraded status with clear reasons when hosted cluster access fails, MetalLB install fails, or service creation fails
- [x] **Unit Test Coverage**: Tests for API validation, MetalLB installation, service creation, and status transitions
- [x] **Security Enhancement**: Updated manager deployment with non-root security context

### Missing/Pending Features
- [x] **External IP Discovery**: Logic to read `.status.loadBalancer.ingress[0].ip` from hosted cluster Service and update `InfraClusterAttachment.status.appsIngressStatus.externalIP` (currently stops at "Pending" phase)
- [x] **DNS Wildcard Records**: Integration with `dnsServerForInfra()` to add `*.apps.<cluster>.<tld>` entries for both VLAN (MetalLB IP) and pod network (proxy IP) views
- [x] **Proxy Wildcard Backend**: Integration with `proxyServerForInfra()` to add SNI wildcard routing for `*.apps.<cluster>.<tld>` on ports 80/443 targeting MetalLB external IP
- [x] **Ready Status Transition**: Logic to transition status from "Pending" → "Ready" when external IP is discovered
- [x] **Requeue Logic**: Backoff/requeue mechanism when external IP is not yet available or during degraded states
- [x] **E2E Tests** (unit tests added, full Kind E2E pending): End-to-end tests simulating full flow with hosted cluster, MetalLB, and traffic validation
- [x] **Documentation** (see docs/apps-ingress.md): User-facing documentation for configuring apps ingress feature in existing docs (QUICKSTART_E2E.md or dedicated guide)

### Areas Needing Clarification
1. **Question about DNS integration**: Should wildcard DNS entries be added automatically to the DNSServer spec in `dnsServerForInfra()`, or should there be a separate reconciliation step that updates DNS after external IP is discovered?
2. **Question about proxy routing**: The plan mentions "policy-based routing" to allow proxy pod to reach external MetalLB IP over VLAN—is this handled by existing NetworkPolicy, or does it require additional configuration?
3. **Question about service selector**: The current implementation uses hardcoded selector `ingresscontroller.operator.openshift.io/deployment-ingresscontroller: default`—is this selector reliable across all HCP deployments, or should it be configurable?
4. **Question about scope boundaries**: Is the current approach of installing MetalLB automatically (rather than just validating existence) the desired behavior? The plan states "user-managed MetalLB" but the implementation installs it.
5. **Question about dual-stack support**: The plan mentions dual-stack as an open question—should IPv6 support be addressed in this PR or deferred to future work?
6. **Question about baseDomain usage**: The attachment's `spec.dns.baseDomain`
   combines with `spec.dns.clusterName` to construct `*.apps.<cluster>.<baseDomain>`.
7. **Question about external IP polling**: Should there be a watch on the hosted cluster Service, or is periodic reconciliation with requeue sufficient for external IP discovery?

## Code Quality Assessment
- **Error Handling**: ✅ Good - Degraded status set with clear reasons and error messages
- **Test Coverage**: ⚠️ Partial - Unit tests cover API and basic reconciliation, but missing E2E tests and integration tests for DNS/Proxy updates
- **Documentation**: ⚠️ Incomplete - Code comments are good, but no user-facing documentation added
- **Logging**: ✅ Good - Uses structured logging via `logf.FromContext(ctx)`
- **Validation**: ✅ Good - API validation via kubebuilder markers, runtime validation for required fields
- **Idempotency**: ✅ Good - Uses `applyUnstructured()` helper for create-or-update pattern

## Suggestions for Improvement
1. **Extract MetalLB installation as optional**: Consider making MetalLB installation opt-in via a boolean flag (e.g., `metallb.autoInstall`) to align with "user-managed" design in plan. Current implementation always installs MetalLB.
2. **Add external IP discovery loop**: Implement logic to poll or watch hosted cluster Service status and update `externalIP` field in status when available.
3. **Integrate with DNS/Proxy generation**: Modify `dnsServerForInfra()` and `proxyServerForInfra()` to conditionally add wildcard entries when `appsIngress.enabled=true` and external IP is known.
4. **Add requeueing with backoff**: Use `ctrl.Result{RequeueAfter: time.Duration}` for pending/degraded states instead of immediate return.
5. **Consider owner references**: Should MetalLB resources in hosted cluster have owner references pointing to the Infra CR for cleanup? (May not be possible cross-cluster)
6. **Add validation webhook**: Consider adding a validating webhook to ensure required fields (e.g., `hostedClusterRef.name`, `metallb.addressPoolName`) are set when `appsIngress.enabled=true`.
7. **Enhance test coverage**: Add integration test that simulates external IP assignment and validates status transition to Ready.
8. **Document MetalLB prerequisites**: Add clear documentation about MetalLB version compatibility, required OLM catalogs, and network configuration prerequisites.

## Next Steps
- [ ] Implement external IP discovery and status update
- [ ] Integrate wildcard DNS entries into DNSServer generation
- [ ] Integrate wildcard proxy backend into ProxyServer generation
- [ ] Add requeue logic for pending external IP
- [ ] Create E2E test validating full apps ingress flow
- [ ] Document feature usage in QUICKSTART_E2E.md or create dedicated apps-ingress guide
- [ ] Clarify scope: should MetalLB installation be automatic or validation-only?
- [ ] Resolve open question: how to construct wildcard domain from baseDomain?


## Completed 2026-08-23
- Implemented discoverAppsIngressExternalIP with IP/hostname handling
- Added DNS wildcard *.apps.<cluster>.<tld> to DNSServer staticEntries (split-horizon)
- Added Proxy wildcard backends apps-http/apps-https with direct IP endpoint (STATIC cluster for IP)
- Added RequeueAfter 15s (Pending) / 30s (Degraded) and Ready transition
- Fixed proxy server to handle STATIC clusters for IP endpoints
- Added unit tests for Ready/Pending transitions and wildcard generation
