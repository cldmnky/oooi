# AGENTS.md

## Stack & Entrypoints
- Kubebuilder v4 / operator-sdk v1.42.0, Go 1.24, controller-runtime v0.20.4. Domain `densityops.com`, API group `hostedcluster.densityops.com/v1alpha1` (see `PROJECT`). Infra CRD in `api/v1alpha1/infra_types.go`, reconciler in `internal/controller/infra_controller.go`.
- Entrypoint `main.go` → `cmd/root.go` (cobra) → `cmd/manager.go` (`go run ./main.go manager`). Subcommands: `cmd/dhcp.go`, `cmd/dns.go`, `cmd/proxy.go`. Libraries in `internal/dhcp|dns|proxy`.

## Layout
- `api/v1alpha1/` — CRD types + `zz_generated.deepcopy.go` (generated, do not edit, header `hack/boilerplate.go.txt`)
- `internal/controller/` — `InfraReconciler` reconciles Infra → creates DHCPServer/DNSServer/ProxyServer + NetworkPolicy (uses `ctrl.SetControllerReference` for GC)
- `config/` — kustomize manifests (`crd/bases`, `rbac/role.yaml`, `manager`, `default`)
- `test/e2e/` + `test/utils/` — Kind-based E2E (Ginkgo/Gomega), NADs in `test/e2e/test-nads.yaml`, Kind configs `hack/kind-config*.yaml`

## Codegen (required order)
- After any `api/v1alpha1/*.go` or RBAC marker change in `internal/controller/*.go` (`// +kubebuilder:rbac:...`):
  `make manifests generate` — `controller-gen` writes CRDs to `config/crd/bases`, RBAC to `config/rbac/role.yaml`, deepcopy to `api/*/zz_generated.deepcopy.go`.
- Do not hand-edit generated files. Verify: `make manifests && cat config/crd/bases/*`.

## Commands — Makefile is source of truth, tools auto-download to `bin/`
- `make fmt vet` — format + vet
- `make test` — unit tests via envtest: `KUBEBUILDER_ASSETS="$(bin/setup-envtest use 1.33.0 --bin-dir ./bin -p path)" go test $(go list ./... | grep -v /e2e) -coverprofile cover.out`
  - Single package: `KUBEBUILDER_ASSETS=$(bin/setup-envtest use 1.33.0 --bin-dir ./bin -p path) go test ./internal/controller -run TestName -v`
- `make lint` / `make lint-fix` / `make lint-config` — golangci-lint v2.1.0 per `.golangci.yml` (excludes `api/*` lll, `internal/*` dupl/lll)
- `make install` / `make uninstall` — CRDs to current `KUBECONFIG` (`kustomize build config/crd | kubectl apply -f -`)
- `make run` — run manager locally against current cluster (`go run ./main.go manager`); requires `make install` first. Debug: `go run ./main.go manager --zap-log-level=debug`
- `make build` — builds `bin/oooi`
- `make container-build` — ko multi-arch `linux/amd64,linux/arm64`; set `IMAGE_TAG_BASE` to choose the registry repository (base `registry.access.redhat.com/ubi9/ubi:9.4` in `.ko.yaml`). E2E variant: `make container-build-e2e`
- `make deploy IMG=<registry>` / `make undeploy` — `kustomize build config/default | kubectl apply -f -`
- `make help` lists all targets. `CONTAINER_TOOL` defaults to `docker` (detects `podman` in some targets).

## E2E — expensive, isolated Kind cluster
- `make setup-test-e2e` — creates/reuses Kind cluster `$KIND_CLUSTER` (default `oooi-test-e2e`; uses `hack/kind-config.yaml` or `hack/kind-config-podman.yaml` if `PODMAN_RUNTIME=true`), installs CNI plugins (`test/e2e/install-cni-plugins.sh`), Calico `v3.29.1`, Multus `v4.2.3`, NodePool/Machine CRD stubs, and NADs (`test/e2e/test-nads.yaml`). Idempotent.
- `make test-e2e` — runs `setup-test-e2e` + `manifests generate fmt vet` + the isolated `KIND_CLUSTER`/`KUBECONFIG` test command, then auto `make cleanup-test-e2e`. Env: `KIND_CLUSTER`, `E2E_KUBECONFIG`, `CALICO_VERSION`, `MULTUS_VERSION`, `PODMAN_RUNTIME`, `E2E_TIMEOUT`, `E2E_FOCUS`, `E2E_IMAGE`, `CERT_MANAGER_INSTALL_SKIP`.
- `make cleanup-test-e2e` / `make cleanup-test-e2e-deep` (+ `docker/podman system prune`)
- Requires: `kind`, `kubectl`, `docker` or `podman`. Namespace `oooi-system` labeled `pod-security.kubernetes.io/enforce=privileged`. CI: `.github/workflows/test-e2e.yml` installs kind+podman and logs into quay.io.

## Testing & CI quirks
- Never `go test ./...` without filtering `/e2e` or without `KUBEBUILDER_ASSETS` — Makefile already does `grep -v /e2e`. Unit tests use Ginkgo/Gomega + envtest (`internal/controller/suite_test.go`, local etcd/apiserver).
- CI: `lint.yml` → `golangci/golangci-lint-action@v8` v2.1.0, `test.yml` → `make test`, `test-e2e.yml` → `make test-e2e`. All on push/PR, ubuntu-latest.
- Lint/tool versions pinned in Makefile: `controller-gen v0.18.0`, `kustomize v5.6.0`, `envtest Kubernetes 1.33.0`, `golangci-lint v2.1.0`.

## Conventions & Gotchas
- RBAC: add `// +kubebuilder:rbac:groups=hostedcluster.densityops.com,resources=...` in controller, then `make manifests` to update `config/rbac/role.yaml`.
- Set owner references on namespaced resources managed by their owning CR for
  cascade deletion. Cross-namespace resources and cluster-scoped DHCP RBAC
  cannot use an `Infra` owner reference and require explicit cleanup checks.
- Logging: use `logf.FromContext(ctx)` (controller-runtime), not `fmt`.
- Base images: `Dockerfile` uses `golang:1.24` builder → `gcr.io/distroless/static:nonroot`; ko build uses UBI9. No manual Dockerfile edits needed for normal flow.
- See `.github/copilot-instructions.md` (preserved critical conventions), `PLAN.md` (VLAN/Envoy/DHCP/DNS design), `QUICKSTART_E2E.md` and `docs/DNS_SETUP.md` for deeper context.
