#!/usr/bin/env bash
# Install additional CNI plugins for Kind cluster (ipvlan, macvlan, etc.)

set -e

CLUSTER_NAME=${1:-oooi-test-e2e}
CNI_VERSION=${2:-v1.9.0}

echo "Installing CNI plugins ${CNI_VERSION} to Kind cluster ${CLUSTER_NAME}..."

# Detect container runtime
if command -v docker &> /dev/null; then
    RUNTIME=docker
elif command -v podman &> /dev/null; then
    RUNTIME=podman
else
    echo "Error: Neither docker nor podman found"
    exit 1
fi

# Download CNI plugins tarball
CNI_PLUGINS_TAR="/tmp/cni-plugins-${CNI_VERSION}.tgz"
if [ ! -f "${CNI_PLUGINS_TAR}" ]; then
    echo "Downloading CNI plugins..."
    ARCH=$(uname -m)
    case "${ARCH}" in
        x86_64) ARCH=amd64 ;;
        aarch64|arm64) ARCH=arm64 ;;
    esac
    
    # Always download linux version for Kind container
    curl -sL "https://github.com/containernetworking/plugins/releases/download/${CNI_VERSION}/cni-plugins-linux-${ARCH}-${CNI_VERSION}.tgz" \
        -o "${CNI_PLUGINS_TAR}"
    
    # Verify download succeeded
    if ! file "${CNI_PLUGINS_TAR}" | grep -q "gzip compressed"; then
        echo "Error: Failed to download CNI plugins tarball"
        cat "${CNI_PLUGINS_TAR}"
        rm -f "${CNI_PLUGINS_TAR}"
        exit 1
    fi
fi

# Copy and extract to Kind node
echo "Installing plugins to Kind node..."
NODE_NAME="${CLUSTER_NAME}-control-plane"

# Create temporary directory in container
${RUNTIME} exec "${NODE_NAME}" mkdir -p /tmp/cni-plugins

# Copy tarball to container
cat "${CNI_PLUGINS_TAR}" | ${RUNTIME} exec -i "${NODE_NAME}" tar -C /tmp/cni-plugins -xzf -

# Copy specific plugins we need to /opt/cni/bin/
for plugin in ipvlan macvlan vlan bridge static host-local; do
    if ${RUNTIME} exec "${NODE_NAME}" test -f "/tmp/cni-plugins/${plugin}"; then
        echo "Installing ${plugin} plugin..."
        ${RUNTIME} exec "${NODE_NAME}" cp "/tmp/cni-plugins/${plugin}" /opt/cni/bin/
        ${RUNTIME} exec "${NODE_NAME}" chmod +x "/opt/cni/bin/${plugin}"
    fi
done

# Cleanup
${RUNTIME} exec "${NODE_NAME}" rm -rf /tmp/cni-plugins

echo "CNI plugins installed successfully"
echo "Available plugins:"
${RUNTIME} exec "${NODE_NAME}" ls -1 /opt/cni/bin/
