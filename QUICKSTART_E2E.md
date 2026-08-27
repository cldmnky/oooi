# E2E quickstart

The E2E suite uses Kind, Calico, Multus, and lightweight NodePool/Machine CRD
stubs. It is an operator integration suite, not a full HyperShift deployment.

## Run it

```bash
make test-e2e
```

The Makefile creates the default `oooi-test-e2e` cluster, installs the CNI
components and test CRDs, runs the tests, and cleans up. For Podman:

```bash
PODMAN_RUNTIME=true make test-e2e
```

Useful overrides:

```bash
KIND_CLUSTER=my-oooi-e2e make test-e2e
E2E_FOCUS='Kubernetes source-IP aliases' make test-e2e
E2E_IMAGE=registry.example.com/oooi:test make test-e2e
```

The suite requires the current kubectl context to be `kind-$KIND_CLUSTER`.

## Prepare only

```bash
export KIND_CLUSTER="${KIND_CLUSTER:-oooi-test-e2e}"
export E2E_KUBECONFIG="${E2E_KUBECONFIG:-$PWD/bin/${KIND_CLUSTER}.kubeconfig}"
export KUBECONFIG="$E2E_KUBECONFIG"
make setup-test-e2e
kubectl get nodes
kubectl get crd nodepools.hypershift.openshift.io machines.cluster.x-k8s.io
kubectl get net-attach-def -A
```

Then run the tests directly and clean up:

```bash
KIND_CLUSTER="$KIND_CLUSTER" KUBECONFIG="$E2E_KUBECONFIG" \
  CERT_MANAGER_INSTALL_SKIP=true go test ./test/e2e/ -v -timeout=20m -ginkgo.v
make cleanup-test-e2e
```

## Coverage

The suite checks controller startup, Multus NADs, Infra child reconciliation,
shared attachment aggregation and cleanup, and source-scoped Kubernetes aliases.
Alias scenarios cover NodePool membership, Machine address filtering, pending
Machine status, address updates, non-KubeVirt pools, and duplicate source IPs.

The stub Machine objects model the address data that CAPK supplies. They do not
replace a target-release test of VMI restart, migration, DHCP renewal, or source
IP preservation on the deployed NAD/CNI path.

## Diagnostics

```bash
kubectl config current-context
kubectl get pods -A
kubectl get nodepool,machine -A
kubectl get net-attach-def -A
```

See [E2E test setup](docs/E2E_TEST_SETUP.md) for prerequisites, environment
variables, file locations, and troubleshooting.

For a real HyperShift/KubeVirt management cluster, see the production-like
validation scripts documented there. The Kind suite does not replace that
workflow's VLAN, public DNS, or worker no-SNI endpoint checks.
