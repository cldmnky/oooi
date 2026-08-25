# E2E coverage summary

This document records the current Kind-based integration-test shape. It is not
a production deployment guide; use [E2E test setup](E2E_TEST_SETUP.md) to run
the suite.

## Environment

The Makefile prepares a Kind cluster with:

- Calico as the test CNI
- Multus and the CNI plugins required by the test NADs
- `test-vlan-100` and `test-vlan-200` NetworkAttachmentDefinitions
- HyperShift `NodePool` and CAPI `Machine` CRD stubs
- The oooi controller image built and loaded by `container-build-e2e`, unless
  `E2E_IMAGE` or `IMG` supplies an image

The suite verifies that it is using `kind-$KIND_CLUSTER` before touching the
cluster. It does not install a full HyperShift control plane or KubeVirt.

## Coverage

### Controller and networking

- Manager Deployment startup and metrics
- Multus installation and test NAD readiness
- Infra reconciliation into DHCPServer, DNSServer, and ProxyServer children
- Static secondary-network annotations and component status
- Attachment lifecycle, conflict handling, and cleanup behavior
- Apps-ingress state transitions where the test fixtures support them

### Source-IP aliases

The source-IP scenario creates two KubeVirt NodePools, attachments, and CAPI
Machines. It verifies:

- The aliases `kubernetes`, `kubernetes.default`,
  `kubernetes.default.svc`, and `kubernetes.default.svc.cluster.local` are
  emitted once in shared DNS when a usable source exists.
- Each generated alias backend has the correct attachment-specific `/32` range.
- Addresses outside the Infra CIDR are ignored.
- A Machine status update replaces the source range.
- A KubeVirt NodePool with no address remains pending without a catch-all alias.
- Non-KubeVirt NodePools do not create alias backends.
- A duplicate source IP sets `DuplicateSourceIP` and suppresses only the
  ambiguous alias backends.

These tests model the management-cluster data consumed by oooi. CAPK's refresh
behavior after a real VMI restart, migration, DHCP renewal, or interface change,
and source-IP preservation through the deployed CNI/NAD, require target-release
validation outside this Kind suite.

## Files

| File | Responsibility |
|---|---|
| `test/e2e/e2e_suite_test.go` | Context safety, CRD/NAD setup, image and Cert-Manager setup |
| `test/e2e/e2e_test.go` | Controller, attachment, apps-ingress, and alias scenarios |
| `test/e2e/crds/nodepools.crd.yaml` | NodePool test schema |
| `test/e2e/machines-status.crd.yaml` | Machine test schema |
| `test/e2e/test-nads.yaml` | Secondary-network test resources |
| `test/utils/utils.go` | Command, Kind, Multus, and resource helpers |
| `Makefile` | Setup, test, image, and cleanup targets |

## Commands

```bash
make setup-test-e2e
make test-e2e
make cleanup-test-e2e
make cleanup-test-e2e-deep
```

Use `KIND_CLUSTER`, `PODMAN_RUNTIME`, `E2E_TIMEOUT`, `E2E_FOCUS`, `E2E_IMAGE`,
and `CERT_MANAGER_INSTALL_SKIP` as documented in
[E2E test setup](E2E_TEST_SETUP.md).
