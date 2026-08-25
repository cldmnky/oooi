# Build images

oooi images are built with [ko](https://ko.build) from the repository root.
Builds are multi-architecture (`linux/amd64` and `linux/arm64`) on a UBI9 base
(see `.ko.yaml`).

## Build and push

```bash
make container-build IMAGE_TAG_BASE=registry.example.com/oooi
```

The target prints a **manifest digest**. Pin that digest everywhere — the
operator image and the component images referenced by `Infra` should be the
same build:

```bash
export OOOI_IMAGE=registry.example.com/oooi@sha256:<digest>
make deploy IMG="$OOOI_IMAGE"
```

## Component images inside Infra

Each child workload is configurable per `Infra` resource:

| Field | Default | Used by |
|---|---|---|
| `infraComponents.dhcp.image` | built-in oooi image for an Infra-generated child | DHCP server |
| `infraComponents.dns.image` | built-in oooi image | DNS server |
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
podman run --rm "$OOOI_IMAGE" --help >/dev/null
docker manifest inspect "registry.example.com/oooi@sha256:<digest>" | head -30
```
