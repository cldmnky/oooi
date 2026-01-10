## Enhanced Plan: Update E2E Tests for Kind Cluster with Multus and KubeVirt

Update the e2e test suite to run on a Kind cluster with Multus CNI and KubeVirt installed, enabling comprehensive testing of the operator's secondary network infrastructure deployment capabilities. This includes detailed implementation steps for setup, configuration, and verification.

### Steps
1. **Modify Makefile for Automated Setup:**
   - Add environment variables: `MULTUS_VERSION ?= v4.2.3`, `KUBEVIRT_VERSION ?= $(shell curl -s https://storage.googleapis.com/kubevirt-prow/release/kubevirt/kubevirt/stable.txt)`
   - Update `setup-test-e2e` target to create Kind cluster with `disableDefaultCNI: true`, then install Multus thick plugin, KubeVirt operator/CR, and enable emulation if needed
   - Add new targets: `install-multus`, `install-kubevirt`, `create-test-nads`

2. **Create Kind Configuration Files:**
   - `hack/kind-config.yaml`: Include `networking: disableDefaultCNI: true` and privileged node settings for KubeVirt
   - `hack/kind-config-podman.yaml`: Similar config with podman-specific settings if `PODMAN_RUNTIME=true`

3. **Add Test NetworkAttachmentDefinitions:**
   - Create `test/e2e/test-nads.yaml` with bridge-type NADs for VLAN 100/200 using `host-local` IPAM for isolated subnets

4. **Enhance Test Utilities:**
   - Add `InstallMultus()`: Apply thick daemonset from `https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/${MULTUS_VERSION}/deployments/multus-daemonset-thick.yml`, wait for readiness
   - Add `InstallKubeVirt()`: Apply operator YAML, CR YAML, patch for emulation, wait for deployments
   - Add `CreateTestNADs()`: Apply NAD manifests
   - Add verification functions: `IsMultusInstalled()`, `IsKubeVirtInstalled()`, `IsNADReady()`
   - Update `LoadImageToKindClusterWithName()` for podman support via `DOCKER_HOST` env var

5. **Update E2E Suite Setup:**
   - In `BeforeSuite`, check/install Multus, KubeVirt, create NADs if missing
   - Add new test contexts for Multus CNI integration (verify daemonsets, NADs) and Infra resource management (create test Infra CR, validate DHCP/DNS/proxy deployments with Multus annotations)

6. **Expand E2E Tests:**
   - **Multus Integration Context:** Verify Multus daemonset running, test NADs created with correct configs
   - **Infra Resource Context:** Create Infra CR with NAD reference, wait for component deployments, verify pods have `k8s.v1.cni.cncf.io/networks` annotations pointing to test NADs

### Further Considerations
1. **Podman Support:** Detect podman availability, use `KIND_EXPERIMENTAL_PROVIDER=podman` if enabled; ensure podman socket accessible
2. **Resource Requirements:** Kind cluster needs 4+ CPUs, 8GB+ RAM for Multus/KubeVirt; increase if tests fail
3. **Cleanup:** Add `cleanup-test-e2e-deep` target to remove Multus/KubeVirt if needed for CI isolation
4. **Version Pinning:** Pin Multus v4.2.3 and latest stable KubeVirt for reproducibility
5. **Testing:** Run locally first; verify Multus NADs allow pod multi-homing before full Infra testing

This enhanced plan provides executable implementation details, ensuring the e2e tests accurately simulate the production environment with secondary networks and VM support.
