# E2E Test Setup Instructions

## Prerequisites

Ensure you have the following installed:
- `kind` (v0.20.0+) - Can be installed via `make kind`
- `kubectl` (matching your cluster version)
- `podman` or `docker` (podman is available on this system)

## Quick Start

### Option 1: Using Docker/Default Setup (Recommended)

```bash
# Create the Kind cluster with Multus and KubeVirt
make setup-test-e2e

# Run the e2e tests
make test-e2e
```

### Option 2: Using Podman Runtime

```bash
# Enable podman runtime explicitly
PODMAN_RUNTIME=true make setup-test-e2e

# Run the e2e tests
make test-e2e
```

## Manual Step-by-Step Setup

If you need to set up manually:

### 1. Install Kind (if not already installed)
```bash
make kind
```

### 2. Create the Kind Cluster
```bash
# Using default config (Docker)
kind create cluster --name oooi-test-e2e --config hack/kind-config.yaml --wait 5m

# Or using podman config
DOCKER_HOST=unix:///run/podman/podman.sock kind create cluster --name oooi-test-e2e --config hack/kind-config-podman.yaml --wait 5m
```

### 3. Install Multus CNI
```bash
make install-multus
```

### 4. Install KubeVirt
```bash
make install-kubevirt
```

### 5. Create Test NetworkAttachmentDefinitions
```bash
make create-test-nads
```

### 6. Build and Deploy the Operator
```bash
make container-build
make deploy IMG=example.com/oooi:v0.0.1
```

### 7. Run E2E Tests
```bash
make test-e2e
```

## Cleanup

### Remove the Kind Cluster
```bash
make cleanup-test-e2e
```

### Deep Cleanup (removes images and volumes)
```bash
make cleanup-test-e2e-deep
```

## Troubleshooting

### Kind Cluster Creation Hangs
- Check if podman/docker is running: `podman ps` or `docker ps`
- Try specifying explicit wait time: `kind create cluster --wait 10m`
- Check Kind version: `kind version`

### Multus/KubeVirt Installation Fails
- Verify cluster is ready: `kubectl cluster-info`
- Check for stuck pods: `kubectl get pods --all-namespaces`
- Check node status: `kubectl get nodes -o wide`

### Network Issues
- Verify NetworkAttachmentDefinitions exist: `kubectl get net-attach-def -A`
- Check CNI plugins: `kubectl get ds -n kube-system`

## Environment Variables

Control the setup behavior with these variables:

- `KIND_CLUSTER`: Name of the Kind cluster (default: `oooi-test-e2e`)
- `MULTUS_VERSION`: Version of Multus CNI (default: `v4.2.3`)
- `KUBEVIRT_VERSION`: Version of KubeVirt (fetched dynamically)
- `PODMAN_RUNTIME`: Use podman instead of Docker (default: `false`)
- `CERT_MANAGER_INSTALL_SKIP`: Skip CertManager installation (default: not set)

## Test Configuration Files

- `hack/kind-config.yaml`: Kind cluster config for Docker
- `hack/kind-config-podman.yaml`: Kind cluster config for Podman
- `test/e2e/test-nads.yaml`: Test NetworkAttachmentDefinitions

## What Gets Tested

The e2e test suite verifies:

1. **Controller Manager**
   - Pod runs successfully
   - Metrics endpoint is serving

2. **Multus CNI Integration**
   - Multus daemonset is running
   - Test NADs are properly configured

3. **Infra Resource Management**
   - Infra resources can be created
   - DHCP/DNS/Proxy components deploy correctly
   - Pods have proper network annotations

## Next Steps

Once the tests pass, you can:
- Integrate into CI/CD pipeline
- Extend tests for your specific use cases
- Monitor KubeVirt VM deployment via secondary networks
