# Installation

Install the oooi operator once per management cluster. It runs in namespace
`oooi-system` and manages `Infra` resources in any namespace it is RBAC-granted
for (by default: cluster-wide).

## Deploy the operator

Set the operator image to the published or mirrored image for your environment:

```bash
export OOOI_IMAGE=registry.example.com/oooi:latest

# 1. CRDs
make install          # = kustomize build config/crd | kubectl apply -f -

# 2. Operator (Deployment, RBAC, namespace)
make deploy IMG="$OOOI_IMAGE"

# 3. Confirm rollout
kubectl -n oooi-system rollout status deployment/oooi-controller-manager --timeout=5m
```

`make deploy` renders `config/default` with kustomize; pin a **digest** for
reproducible environments:

```bash
export OOOI_IMAGE=registry.example.com/oooi@sha256:<digest>
```

Detailed behavior, RBAC, and troubleshooting:
[Deploy the operator](deploy-operator.md).

## Build your own images

If you cannot consume the published image, build and push with ko (multi-arch,
UBI9-based):

```bash
make container-build IMAGE_TAG_BASE=registry.example.com/oooi
```

See [Build images](build-images.md).

## Verify the installation

```bash
kubectl get crd | grep hostedcluster.densityops.com
# dhcpservers.hostedcluster.densityops.com
# dnsservers.hostedcluster.densityops.com
# infras.hostedcluster.densityops.com
# infraclusterattachments.hostedcluster.densityops.com
# proxyservers.hostedcluster.densityops.com

kubectl -n oooi-system get deploy,pods
```

## Upgrade

1. Build or pull the new image.
2. Re-run `make install` (CRD schema updates).
3. Re-run `make deploy IMG="$NEW_IMAGE"` — the Deployment is updated in place.
4. Update `proxyImage` / `managerImage` fields in existing `Infra` resources if
   you pin component images there.

Existing child resources reconcile to the new spec on the next sync; no data
migration is required.

## Uninstall

Follow [Uninstall and cleanup](../operations/uninstall.md) — delete
`InfraClusterAttachment` resources first so hosted-cluster resources and
cross-namespace policies are cleaned up, then delete `Infra` resources and
remove the operator and CRDs.
