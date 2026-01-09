# Plan: Implement Core Infrastructure Components for HCP Operator

This plan implements the foundational infrastructure services (DHCP, DNS, Envoy) needed to bridge isolated VLAN networks with OpenShift Hosted Control Planes on OpenShift Virtualization, based on the comprehensive architectural design documented in PLAN.md.

## Steps

1. **Design and implement API types** - Replace placeholder `Foo` field in api/v1alpha1/infra_types.go with complete `InfraSpec` and `InfraStatus` including `NetworkConfig` (CIDR, gateway, NAD reference), `InfraComponents` (DHCP/DNS/Proxy configs), and status conditions. Run `make manifests generate` to update CRDs and deepcopy code.

2. **Create IP allocation utility package** - Build `pkg/ipam/` package with functions to parse CIDR blocks, calculate subnet ranges, and allocate static IPs for infrastructure components (CoreDNS, Envoy, DHCP server) while reserving DHCP pool ranges to avoid conflicts.

3. **Implement DHCP component module** - Create `internal/dhcp/` with `config.go` (generate `dhcpd.conf` using Go templates from InfraSpec), `deployment.go` (build StatefulSet with PVC for lease persistence), and unit tests. Include Multus annotations for secondary network attachment and Policy-Based Routing initialization script for asymmetric routing.

4. **Implement CoreDNS split-horizon module** - Create `internal/dns/` with `corefile.go` (generate Corefile with static A-records pointing control plane FQDNs to Envoy IP), `deployment.go` (build dual-homed Deployment), and tests. Configure `hosts` plugin for api/api-int/\*.apps domains and `forward` plugin for external DNS resolution.

5. **Implement Envoy L4 proxy module** - Create `internal/proxy/` with `envoy_config.go` (generate Envoy static config for TCP listeners on ports 6443/API, 443/OAuth, 22623/Ignition, 8132/Konnectivity), `deployment.go` (build dual-homed Deployment with PBR initContainer), and tests. Use LOGICAL_DNS cluster type for upstream control plane services in `clusters-*` namespace.

6. **Implement main reconciliation loop** - Update internal/controller/infra_controller.go `Reconcile()` to fetch Infra CR, validate NetworkAttachmentDefinition exists, orchestrate DHCP/DNS/Envoy sub-reconcilers, set OwnerReferences for garbage collection, update status conditions (Ready/Provisioning/Failed), and handle errors with proper logging and requeue logic.

## Further Considerations

1. **RBAC and Security Context** - Need to expand config/rbac/role.yaml for `k8s.cni.cncf.io` (NetworkAttachmentDefinitions), `apps` (Deployments/StatefulSets), core resources, and create custom SecurityContextConstraints allowing NET_ADMIN capability for PBR. Should we use privileged SCC or create a custom one with minimal required capabilities?

2. **Testing Strategy** - Start with unit tests for IPAM/config generation, then integration tests using envtest in internal/controller/infra_controller_test.go. E2E tests in test/e2e/ require real OpenShift cluster with HyperShift and OVN-Kubernetes. Prioritize local tests first, or also setup CI pipeline for e2e validation?

3. **Multus Integration Approach** - Need to add `github.com/k8snetworkplumbingwg/network-attachment-definition-client` dependency and create `internal/network/multus.go` helpers for annotation generation and status parsing. Should the operator create/manage NetworkAttachmentDefinitions, or only validate their existence and reference them?
