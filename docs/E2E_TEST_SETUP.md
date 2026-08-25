# E2E test setup

The E2E suite runs in an isolated Kind cluster. It validates oooi controller
reconciliation, Multus secondary networks, shared attachments, and the
source-IP alias aggregation logic using NodePool and CAPI Machine CRD stubs.
It does not create a production HyperShift or KubeVirt environment.

## Prerequisites

- `kind` (the Makefile can download it into `./bin`)
- `kubectl`
- Docker or Podman
- Go 1.24+ and a working Go toolchain

## Quick start

```bash
make test-e2e
```

The target creates or reuses `oooi-test-e2e`, installs Calico and Multus,
installs the test NodePool and Machine CRDs, creates the test NADs, runs
`test/e2e`, and removes the Kind cluster afterward. Set `KIND_CLUSTER` to use a
different cluster name.

Use Podman explicitly:

```bash
PODMAN_RUNTIME=true make test-e2e
```

Use a prebuilt image or focus one Ginkgo context:

```bash
E2E_IMAGE=registry.example.com/oooi:test \
  E2E_FOCUS='Kubernetes source-IP aliases' \
  make test-e2e
```

The default `E2E_TIMEOUT` is 20 minutes. `CERT_MANAGER_INSTALL_SKIP=true` is
set by the Makefile; pass a different value only when running the Go test
directly.

## Setup without running tests

```bash
export KIND_CLUSTER="${KIND_CLUSTER:-oooi-test-e2e}"
export E2E_KUBECONFIG="${E2E_KUBECONFIG:-$PWD/bin/${KIND_CLUSTER}.kubeconfig}"
export KUBECONFIG="$E2E_KUBECONFIG"
make setup-test-e2e
kubectl config current-context
kubectl get nodes
kubectl get crd nodepools.hypershift.openshift.io machines.cluster.x-k8s.io
kubectl get net-attach-def -A
```

`setup-test-e2e` writes the isolated Kind kubeconfig, installs the CNI plugins,
waits for the Calico and Multus components as configured by the Makefile,
installs the NodePool/Machine CRD stubs, and applies `test/e2e/test-nads.yaml`.

Run the suite against the prepared cluster with cleanup handled manually:

```bash
KIND_CLUSTER="$KIND_CLUSTER" KUBECONFIG="$E2E_KUBECONFIG" \
  CERT_MANAGER_INSTALL_SKIP=true go test ./test/e2e/ -v -timeout=20m -ginkgo.v
make cleanup-test-e2e
```

The suite refuses to run unless the current kubectl context is
`kind-$KIND_CLUSTER`.

## What is tested

- Controller manager startup and metrics
- Calico, Multus, and the test NetworkAttachmentDefinitions
- Infra creation and shared DHCP/DNS/proxy child reconciliation
- `InfraClusterAttachment` aggregation, conflict exclusion, and cleanup
- Apps-ingress status and hosted-resource cleanup paths where applicable
- Source-scoped `kubernetes.*` aliases from NodePool and Machine objects
- Machine address filtering by the Infra CIDR
- Machine address updates and pending address recovery
- Duplicate source-IP handling with `DuplicateSourceIP`

The alias tests model CAPK output by writing `Machine.status.addresses`; they do
not prove that a particular OpenShift release refreshes those addresses after a
real VMI restart or migration. Validate that behavior separately on the target
HyperShift/CAPK release.

## Relevant files

| File | Purpose |
|---|---|
| `hack/kind-config.yaml` | Docker Kind configuration |
| `hack/kind-config-podman.yaml` | Podman Kind configuration |
| `test/e2e/test-nads.yaml` | Test secondary networks |
| `test/e2e/crds/nodepools.crd.yaml` | HyperShift NodePool stub |
| `test/e2e/machines-status.crd.yaml` | CAPI Machine stub |
| `test/e2e/e2e_suite_test.go` | Cluster and suite setup |
| `test/e2e/e2e_test.go` | E2E scenarios |
| `test/utils/utils.go` | Cluster and command helpers |
| `Makefile` | Supported targets and defaults |

## Cleanup

```bash
make cleanup-test-e2e
make cleanup-test-e2e-deep
```

`cleanup-test-e2e-deep` also prunes container images and volumes through the
available Docker/Podman commands. Run it only when those caches are disposable.

## Troubleshooting

### Wrong kubectl context

```bash
kubectl config current-context
kind get clusters
kubectl config use-context kind-${KIND_CLUSTER:-oooi-test-e2e}
```

The E2E suite intentionally fails instead of modifying a non-Kind cluster.

### CNI or NAD is not ready

```bash
kubectl get pods -n kube-system
kubectl get ds -n kube-system
kubectl get net-attach-def -A
kubectl describe net-attach-def test-vlan-100 -n oooi-system
```

Re-run `make setup-test-e2e` to reconcile the setup. It is designed to be
idempotent for an existing Kind cluster.

### Alias tests stay pending

```bash
kubectl get nodepool -A
kubectl get machine -A -o yaml
kubectl -n oooi-system get proxyserver -o yaml
```

Check that the Machine annotation is
`hypershift.openshift.io/nodePool=<namespace>/<name>` and that at least one
address is inside the test Infra CIDR. Addresses outside that CIDR are
intentionally ignored.
