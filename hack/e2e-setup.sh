#!/bin/bash
# E2E Test Complete Flow Script
# This script demonstrates the full setup and execution flow for e2e tests

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
KIND_CLUSTER="${KIND_CLUSTER:-oooi-test-e2e}"
RUNTIME="${RUNTIME:-docker}" # or podman

echo -e "${YELLOW}=== OOOI E2E Test Complete Flow ===${NC}"
echo ""

# Step 1: Verify prerequisites
echo -e "${YELLOW}Step 1: Verifying prerequisites...${NC}"
if ! command -v kind &> /dev/null; then
    echo -e "${RED}✗ kind is not installed${NC}"
    echo "Install with: make kind"
    exit 1
fi
echo -e "${GREEN}✓ kind is installed ($(kind version | head -1))${NC}"

if ! command -v kubectl &> /dev/null; then
    echo -e "${RED}✗ kubectl is not installed${NC}"
    exit 1
fi
echo -e "${GREEN}✓ kubectl is available${NC}"

if ! command -v podman &> /dev/null && ! command -v docker &> /dev/null; then
    echo -e "${RED}✗ Neither podman nor docker is installed${NC}"
    exit 1
fi

CONTAINER_CMD="docker"
if command -v podman &> /dev/null && [ "$RUNTIME" = "podman" ]; then
    CONTAINER_CMD="podman"
fi
echo -e "${GREEN}✓ Container runtime available: $CONTAINER_CMD${NC}"

echo ""

# Step 2: Check if cluster already exists
echo -e "${YELLOW}Step 2: Checking for existing Kind cluster...${NC}"
if kind get clusters | grep -q "$KIND_CLUSTER"; then
    echo -e "${GREEN}✓ Kind cluster '$KIND_CLUSTER' already exists${NC}"
    CLUSTER_EXISTS=true
else
    echo -e "${YELLOW}○ Kind cluster '$KIND_CLUSTER' does not exist, will be created${NC}"
    CLUSTER_EXISTS=false
fi

echo ""

# Step 3: Create cluster if needed
if [ "$CLUSTER_EXISTS" = false ]; then
    echo -e "${YELLOW}Step 3: Creating Kind cluster...${NC}"
    CONFIG_FILE="hack/kind-config.yaml"
    if [ "$RUNTIME" = "podman" ]; then
        CONFIG_FILE="hack/kind-config-podman.yaml"
        export DOCKER_HOST="unix:///run/podman/podman.sock"
    fi
    
    kind create cluster --name "$KIND_CLUSTER" --config "$CONFIG_FILE" --wait 5m
    echo -e "${GREEN}✓ Kind cluster created successfully${NC}"
    echo ""
    
    # Step 4: Install CNI plugins (ipvlan, static IPAM, etc.)
    echo -e "${YELLOW}Step 4: Installing CNI plugins...${NC}"
    make install-cni-plugins
    echo -e "${GREEN}✓ CNI plugins installed${NC}"
    echo ""
    
    # Step 5: Install Multus CNI
    echo -e "${YELLOW}Step 5: Installing Multus CNI...${NC}"
    make install-multus
    echo -e "${GREEN}✓ Multus CNI installed${NC}"
    echo ""
    
    # Step 6: Install KubeVirt
    echo -e "${YELLOW}Step 6: Installing KubeVirt...${NC}"
    make install-kubevirt
    echo -e "${GREEN}✓ KubeVirt installed${NC}"
    echo ""
    
    # Step 7: Create test NADs
    echo -e "${YELLOW}Step 7: Creating test NetworkAttachmentDefinitions...${NC}"
    make create-test-nads
    echo -e "${GREEN}✓ Test NADs created${NC}"
    echo ""
else
    echo -e "${YELLOW}Step 3-7: Cluster already exists, skipping setup${NC}"
    echo ""
fi

# Step 8: Wait for cluster to be ready
echo -e "${YELLOW}Step 8: Waiting for cluster to be ready...${NC}"
kubectl wait --for=condition=Ready node --all --timeout=5m || true
echo -e "${GREEN}✓ Cluster is ready${NC}"
echo ""

# Step 9: Verify Multus and KubeVirt
echo -e "${YELLOW}Step 9: Verifying Multus and KubeVirt...${NC}"
if kubectl get daemonset kube-multus-ds -n kube-system &> /dev/null; then
    echo -e "${GREEN}✓ Multus daemonset found${NC}"
else
    echo -e "${YELLOW}○ Multus daemonset not found (may still be deploying)${NC}"
fi

if kubectl get kubevirt kubevirt -n kubevirt &> /dev/null; then
    echo -e "${GREEN}✓ KubeVirt found${NC}"
else
    echo -e "${YELLOW}○ KubeVirt not found (may still be deploying)${NC}"
fi
echo ""

# Step 10: Show cluster info
echo -e "${YELLOW}Step 10: Cluster information...${NC}"
echo "Cluster name: $KIND_CLUSTER"
echo "Kubeconfig: ${KUBECONFIG:-$HOME/.kube/config}"
echo ""
echo "Nodes:"
kubectl get nodes -o wide
echo ""
echo "Namespaces with Multus/KubeVirt:"
kubectl get namespace kube-system kubevirt oooi-system 2>/dev/null || echo "Some namespaces not ready yet"
echo ""

# Step 11: Ready for E2E tests
echo -e "${GREEN}=== Setup Complete ===${NC}"
echo ""
echo -e "${YELLOW}To run the E2E tests, execute:${NC}"
echo "  make test-e2e"
echo ""
echo -e "${YELLOW}To cleanup the cluster, execute:${NC}"
echo "  make cleanup-test-e2e"
echo ""
echo -e "${YELLOW}For more information, see:${NC}"
echo "  docs/E2E_TEST_SETUP.md"
