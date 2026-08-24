# Build images

oooi images are built with [ko](https://ko.build) from the repository root.
Builds are multi-architecture (`linux/amd64` and `linux/arm64`) on a
UBI9 base (`registry.access.redhat.com/ubi9/ubi:9.4`, see `.ko.yaml`).

## Build and push

```bash
export KO_DOCKER_REPO=quay.io/example/oooi   # any registry you can push to
make container-build
```

The target prints a **manifest digest**. Pin that digest everywhere — the
operator image and the component images referenced by `Infra` should be the
same build:

```bash
export OOOI_IMAGE=quay.io/example/oooi@sha256:<digest>
make deploy IMG="$OOOI_IMAGE"
```

## Component images inside Infra

Each child workload is configurable per `Infra` resource:

| Field | Default | Used by |
|---|---|---|
| `infraComponents.dhcp.image` | built-in oooi image | DHCP server |
| `infraComponents.dns.image` | CoreDNS | DNS server |
| `infraComponents.proxy.proxyImage` | `envoyproxy/envoy:v1.36.4` | Envoy container |
| `infraComponents.proxy.managerImage` | oooi image | xDS manager sidecar |

!!! tip

    In air-gapped registries, mirror both the oooi image and the Envoy image,
    then set these fields explicitly. oooi reconciles them, so changing the
    field updates (and restarts) the Deployment.

## Local binary

```bash
make build          # produces bin/oooi
./bin/oooi --help   # subcommands: manager, dhcp, dns, proxy
```

## Verifying a build

```bash
podman run --rm "$OOOI_IMAGE" version 2>/dev/null || true
docker manifest inspect "quay.io/example/oooi@sha256:<digest>" | head -30
```
