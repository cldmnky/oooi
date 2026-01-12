#!/bin/bash
# Debug script for troubleshooting Envoy proxy configuration
# Usage: ./hack/debug-proxy.sh [cluster-name]

set -e

CLUSTER_NAME="${1:-species-8472}"
NAMESPACE="clusters"

echo "==> Finding proxy pod for cluster: ${CLUSTER_NAME}"
POD_NAME=$(oc get pods -n ${NAMESPACE} -l hostedcluster.densityops.com=${CLUSTER_NAME}-proxy -o name | head -1)

if [ -z "$POD_NAME" ]; then
    echo "Error: No proxy pod found for cluster ${CLUSTER_NAME}"
    exit 1
fi

echo "==> Using pod: ${POD_NAME}"
echo ""

echo "==> Proxy listeners (external ports):"
oc exec -n ${NAMESPACE} ${POD_NAME} -c manager -- curl -s localhost:9901/config_dump | \
    jq -r '.configs[] | select(.["@type"] | contains("Listener")) | .dynamic_listeners[] | 
    "Listener: \(.active_state.listener.name) → Port: \(.active_state.listener.address.socket_address.port_value)\n"'

echo ""
echo "==> Cluster backends (internal service endpoints):"
oc exec -n ${NAMESPACE} ${POD_NAME} -c manager -- curl -s localhost:9901/config_dump | \
    jq -r '.configs[] | select(.["@type"] | contains("Cluster")) | .dynamic_active_clusters[] | 
    "Cluster: \(.cluster.name)\n  → \(.cluster.load_assignment.endpoints[].lb_endpoints[].endpoint.address.socket_address.address):\(.cluster.load_assignment.endpoints[].lb_endpoints[].endpoint.address.socket_address.port_value)\n"'

echo ""
echo "==> SNI routing (hostname → listening port → cluster → backend port):"
oc exec -n ${NAMESPACE} ${POD_NAME} -c manager -- curl -s localhost:9901/config_dump | \
    jq -r '.configs[] | select(."@type" | contains("Listener")) | .dynamic_listeners[].active_state.listener as $listener | ($listener.filter_chains[] | select(.filter_chain_match.server_names != null) | "SNI: \(.filter_chain_match.server_names | join(", "))\n  → Listener Port: \($listener.address.socket_address.port_value)\n  → Cluster: \(.filters[].typed_config.cluster)\n")'

echo ""
echo "==> Fallback chains (non-SNI → listening port → cluster):"
oc exec -n ${NAMESPACE} ${POD_NAME} -c manager -- curl -s localhost:9901/config_dump | \
    jq -r '.configs[] | select(."@type" | contains("Listener")) | .dynamic_listeners[].active_state.listener as $listener | ($listener.filter_chains[] | select(.filter_chain_match == null or .filter_chain_match.server_names == null or (.filter_chain_match.server_names | length) == 0) | "Fallback (non-SNI)\n  → Listener Port: \($listener.address.socket_address.port_value)\n  → Cluster: \(.filters[].typed_config.cluster)\n")'

echo ""
echo "==> Proxy pod status:"
oc get ${POD_NAME} -n ${NAMESPACE} -o wide
