# oooi - OpenShift Hosted Control Plane Infrastructure Operator

Kubernetes operator for deploying infrastructure components required by OpenShift Hosted Control Planes (HCP) running on OpenShift Virtualization with isolated secondary networks (VLANs).

## Architecture

**Core Problem**: When KubeVirt VMs run with `attach-default-network: false` on isolated VLANs, they lack direct connectivity to the hosted control plane services running on the management cluster's pod network. This operator bridges that gap by deploying infrastructure services (DHCP, DNS, L4 proxy) onto the secondary network.

**Key Components**:
- **Infra CRD** ([api/v1alpha1/infra_types.go](api/v1alpha1/infra_types.go)): Custom resource defining infrastructure requirements for a hosted cluster
- **InfraReconciler** ([internal/controller/infra_controller.go](internal/controller/infra_controller.go)): Controller that watches `HostedCluster` resources and provisions DHCP/DNS/Envoy services
- **API Group**: `hostedcluster.densityops.com/v1alpha1` (see [PROJECT](PROJECT))

**Reference Architecture**: See [PLAN.md](PLAN.md) for comprehensive design document covering network isolation requirements, policy-based routing, and service topology.

## Kubebuilder Project Structure

This is a standard Kubebuilder v4 operator scaffolded with Operator SDK plugins:
- `api/v1alpha1/`: API type definitions and generated deepcopy code
- `cmd/main.go`: Operator entrypoint with manager setup
- `internal/controller/`: Reconciliation logic and controller tests
- `config/`: Kustomize manifests for CRDs, RBAC, deployment
- `test/e2e/`: End-to-end tests using Kind clusters

## Essential Make Targets

**Development cycle**:
```bash
make manifests generate  # Regenerate CRDs and deepcopy after API changes
make fmt vet            # Format and vet code
make test               # Run unit tests with envtest (auto-downloads K8s binaries)
make lint               # Run golangci-lint (version 2.1.0)
```

**Local testing**:
```bash
make install            # Install CRDs to ~/.kube/config cluster
make run               # Run controller locally against cluster
make uninstall         # Remove CRDs
```

**E2E testing** (requires `kind`):
```bash
make test-e2e          # Creates Kind cluster, runs e2e tests, tears down
```
- Cluster name: `oooi-test-e2e` (configurable via `KIND_CLUSTER`)
- Tests verify CRD installation, controller deployment, metrics endpoint
- Auto-cleans up on completion

**Container workflow**:
```bash
make docker-build IMG=densityops.com/oooi:v0.0.1
make docker-push IMG=densityops.com/oooi:v0.0.1
make deploy IMG=densityops.com/oooi:v0.0.1
```

## Critical Conventions

**RBAC markers**: Use kubebuilder RBAC comments in controller files:
```go
// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=infras,verbs=get;list;watch
```
Then regenerate with `make manifests` to update [config/rbac/role.yaml](config/rbac/role.yaml).

**Owner references**: All operator-created resources must set `OwnerReference` pointing to the `HostedCluster` to enable garbage collection cascade.

**Testing pattern**: Controller tests use Ginkgo/Gomega with envtest ([internal/controller/suite_test.go](internal/controller/suite_test.go)). The test suite starts a local etcd/apiserver and fake client.

**Logging**: Use `logf.FromContext(ctx)` from controller-runtime for structured logging, not direct `fmt.Println`.

## Dependencies & Tools

**Auto-downloaded tools** (to `bin/`):
- `controller-gen` v0.18.0: Generates CRDs, RBAC, webhooks
- `setup-envtest`: Downloads Kubernetes test binaries for unit tests
- `kustomize` v5.6.0: Builds Kubernetes manifests
- `golangci-lint` v2.1.0: Linting

**Manual prerequisites**:
- Go 1.24+
- `kubectl` or `oc` CLI
- `kind` (for e2e tests)
- Docker/Podman (for container builds)

## Debugging Tips

**Run controller with verbose logs**:
```bash
make run -- --zap-log-level=debug
```

**View generated manifests**:
```bash
make manifests && cat config/crd/bases/*
```

**Test against remote cluster**:
```bash
export KUBECONFIG=/path/to/kubeconfig
make install deploy IMG=your-registry/oooi:dev
kubectl logs -n oooi-system deployment/oooi-controller-manager
```</content>
<parameter name="filePath">/Users/mbengtss/code/src/github.com/cldmnky/oooi/.github/copilot-instructions.md