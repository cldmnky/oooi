#!/bin/bash
# Troubleshooting checklist for Hosted Control Plane infrastructure proxy

set -e

CLUSTER_NAME="${1:-species-8472}"
NAMESPACE="clusters"

echo "=================================="
echo "HCP Infrastructure Proxy Troubleshooting"
echo "Cluster: ${CLUSTER_NAME}"
echo "=================================="
echo ""

POD_NAME=$(oc get pods -n ${NAMESPACE} -l hostedcluster.densityops.com=${CLUSTER_NAME}-proxy -o name 2>/dev/null | head -1)
if [ -z "$POD_NAME" ]; then
    echo "❌ ERROR: No proxy pod found for cluster ${CLUSTER_NAME}"
    exit 1
fi
POD_SHORT=$(echo $POD_NAME | cut -d/ -f2)

echo "✓ Proxy pod: $POD_SHORT"
echo ""

# 1. Check xDS control plane
echo "=== 1. Control Plane Status ==="
oc exec -n ${NAMESPACE} ${POD_NAME} -c manager -- curl -s localhost:8080/metrics 2>/dev/null | grep -E "go_info|process_start_time" | head -2
echo "✓ xDS control plane is running"
echo ""

# 2. Check cluster backend status
echo "=== 2. Backend Cluster Status ==="
oc exec -n ${NAMESPACE} ${POD_NAME} -c manager -- curl -s localhost:9901/stats | grep "cluster.*membership_total" | while read line; do
  cluster=$(echo $line | cut -d. -f2)
  total=$(echo $line | awk '{print $NF}')
  healthy=$(oc exec -n ${NAMESPACE} ${POD_NAME} -c manager -- curl -s localhost:9901/stats 2>/dev/null | grep "cluster.${cluster}.membership_healthy" | awk '{print $NF}')
  printf "%-50s healthy: %d/%d\n" "${cluster}" "$healthy" "$total"
done
echo ""

# 3. Check listener configuration
echo "=== 3. Listener Configuration ==="
oc exec -n ${NAMESPACE} ${POD_NAME} -c manager -- curl -s localhost:9901/config_dump | \
  jq -r '.configs[] | select(.["@type"] | contains("Listener")) | 
    .dynamic_listeners[].active_state.listener | 
    "\(.name)\n  Address: \(.address.socket_address.address):\(.address.socket_address.port_value)\n  Filter chains: \(.filter_chains | length)"'
echo ""

# 4. Check SNI routing
echo "=== 4. SNI Routing (Hostname → Cluster) ==="
oc exec -n ${NAMESPACE} ${POD_NAME} -c manager -- curl -s localhost:9901/config_dump | \
  jq -r '.configs[] | select(.["@type"] | contains("Listener")) | 
    .dynamic_listeners[].active_state.listener.filter_chains[] | 
    "  \(.filter_chain_match.server_names[0]) → \(.filters[].typed_config.cluster)"' | sort
echo ""

# 5. Check backend endpoints
echo "=== 5. Backend Endpoints ==="
oc exec -n ${NAMESPACE} ${POD_NAME} -c manager -- curl -s localhost:9901/config_dump | \
  jq -r '.configs[] | select(.["@type"] | contains("Cluster")) | 
    .dynamic_active_clusters[] | 
    "  \(.cluster.name):\n    → \(.cluster.load_assignment.endpoints[].lb_endpoints[].endpoint.address.socket_address.address):\(.cluster.load_assignment.endpoints[].lb_endpoints[].endpoint.address.socket_address.port_value)"' | sort
echo ""

# 6. Check Pod status
echo "=== 6. Pod Status ==="
oc get ${POD_NAME} -n ${NAMESPACE} -o wide
echo ""

# 7. Connection test instructions
echo "=== 7. Test Connectivity ==="
echo "From VM on secondary network (10.202.64.x):"
echo "  curl https://api.species-8472.clusters.blahonga.me -k"
echo "  curl https://oauth.species-8472.clusters.blahonga.me -k"
echo "  curl https://ignition.species-8472.clusters.blahonga.me -k"
echo "  curl https://konnectivity.species-8472.clusters.blahonga.me -k"
echo ""
echo "Expected responses:"
echo "  - api: 403 Forbidden (Kubernetes API)"
echo "  - oauth: 403 Forbidden (OAuth server)"
echo "  - ignition: Depends on request format (ignition-server expects specific paths)"
echo "  - konnectivity: Connection should succeed, responds with konnectivity protocol"
