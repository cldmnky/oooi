#!/usr/bin/env bash
set -Eeuo pipefail

REPO_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

KUBE_CONTEXT="${KUBE_CONTEXT:-default/api-borg-blahonga-me:6443/kube:admin}"
MANAGEMENT_NAMESPACE="${MANAGEMENT_NAMESPACE:-clusters}"
CLUSTER_NAME="${CLUSTER_NAME:-species-8472}"
SECOND_CLUSTER_NAME="${SECOND_CLUSTER_NAME:-two-of-two}"
INFRA_NAME="${INFRA_NAME:-vlan203}"
BASE_DOMAIN="${BASE_DOMAIN:-clusters.blahonga.me}"
CLUSTER_DOMAIN="${CLUSTER_NAME}.${BASE_DOMAIN}"
SECOND_CLUSTER_DOMAIN="${SECOND_CLUSTER_NAME}.${BASE_DOMAIN}"
SETUP_RELEASE="${SETUP_RELEASE:-oooi-end-user-setup}"
OOOI_NAMESPACE="${OOOI_NAMESPACE:-oooi-system}"
EXTERNAL_DNS_NAMESPACE="${EXTERNAL_DNS_NAMESPACE:-external-dns-operator}"
ATTACHMENT_DELETE_WAIT="${ATTACHMENT_DELETE_WAIT:-900}"
HOSTED_CLUSTER_DELETE_WAIT="${HOSTED_CLUSTER_DELETE_WAIT:-3600}"
HELM_UNINSTALL_TIMEOUT="${HELM_UNINSTALL_TIMEOUT:-1h}"
GC_WAIT="${GC_WAIT:-300}"
DNS_CLEANUP_WAIT="${DNS_CLEANUP_WAIT:-600}"
POLL_INTERVAL="${POLL_INTERVAL:-10}"
PUBLIC_DNS_SERVER="${PUBLIC_DNS_SERVER:-}"
VERIFY_DNS_CLEANUP="${VERIFY_DNS_CLEANUP:-true}"

DHCP_NAME="${INFRA_NAME}-dhcp"
DNS_NAME="${INFRA_NAME}-dns"
PROXY_NAME="${INFRA_NAME}-proxy"
PRIMARY_EXTERNAL_PROXY_SERVICE="${CLUSTER_NAME}-proxy-external"
SECOND_EXTERNAL_PROXY_SERVICE="${SECOND_CLUSTER_NAME}-proxy-external"
INFRA_LABEL_KEY="hostedcluster.densityops.com/network-policy-group"
INFRA_LABEL_MARKER="oooi.densityops.com/e2e-infrastructure-label"

log() {
  printf '[oooi-cleanup] %s\n' "$*"
}

fail() {
  printf '[oooi-cleanup] ERROR: %s\n' "$*" >&2
  exit 1
}

kube() {
  kubectl --context "$KUBE_CONTEXT" "$@"
}

helm_kube() {
  helm --kube-context "$KUBE_CONTEXT" "$@"
}

require_commands() {
  local binary
  for binary in "$@"; do
    command -v "$binary" >/dev/null 2>&1 || fail "required command not found: $binary"
  done
}

wait_deleted() {
  local description="$1" timeout="$2" resource="$3" name="$4" namespace="${5:-}"
  local deadline=$((SECONDS + timeout))
  while (( SECONDS < deadline )); do
    if [[ -n "$namespace" ]]; then
      if ! kube -n "$namespace" get "$resource" "$name" >/dev/null 2>&1; then
        log "$description"
        return 0
      fi
    elif ! kube get "$resource" "$name" >/dev/null 2>&1; then
      log "$description"
      return 0
    fi
    sleep "$POLL_INTERVAL"
  done
  printf '[oooi-cleanup] WARNING: timed out waiting for %s\n' "$description" >&2
  return 1
}

wait_namespace_deleted() {
  local namespace="$1" timeout="$2"
  local deadline=$((SECONDS + timeout))
  while (( SECONDS < deadline )); do
    if ! kube get namespace "$namespace" >/dev/null 2>&1; then
      log "namespace $namespace deleted"
      return 0
    fi
    sleep "$POLL_INTERVAL"
  done
  printf '[oooi-cleanup] WARNING: timed out waiting for namespace %s to be deleted\n' "$namespace" >&2
  return 1
}

delete_namespaced() {
  local resource="$1" name="$2" namespace="$3"
  kube -n "$namespace" delete "$resource" "$name" --ignore-not-found --wait=false || true
}

delete_cluster_scoped() {
  local resource="$1" name="$2"
  kube delete "$resource" "$name" --ignore-not-found --wait=false || true
}

remove_attachment_finalizer() {
  local name="$1"
  kube -n "$MANAGEMENT_NAMESPACE" patch infraattachment "$name" \
    --type=merge -p '{"metadata":{"finalizers":[]}}' >/dev/null 2>&1 || true
}

public_dns_answers() {
  local host="$1"
  if [[ -n "$PUBLIC_DNS_SERVER" ]]; then
    dig +short +time=3 +tries=1 "@$PUBLIC_DNS_SERVER" "$host" A 2>/dev/null || true
  else
    dig +short +time=3 +tries=1 "$host" A 2>/dev/null || true
  fi
}

public_dns_absent() {
  local host="$1"
  [[ -z "$(public_dns_answers "$host")" ]]
}

remove_test_namespace_label() {
  local namespace="$1" marker
  marker="$(kube get namespace "$namespace" -o jsonpath="{.metadata.annotations['$INFRA_LABEL_MARKER']}" 2>/dev/null || true)"
  if [[ "$marker" == "true" ]]; then
    kube label namespace "$namespace" "${INFRA_LABEL_KEY}-" >/dev/null 2>&1 || true
    kube annotate namespace "$namespace" "${INFRA_LABEL_MARKER}-" >/dev/null 2>&1 || true
    log "Removed the infrastructure label added to namespace $namespace"
  fi
}

dump_status() {
  kube -n "$MANAGEMENT_NAMESPACE" get hostedcluster,nodepool,infra,infraattachment,dhcpserver,dnsserver,proxyserver -o wide || true
  kube -n "$MANAGEMENT_NAMESPACE" get service \
    "$PRIMARY_EXTERNAL_PROXY_SERVICE" \
    "$SECOND_EXTERNAL_PROXY_SERVICE" \
    "$DNS_NAME" \
    "$PROXY_NAME" -o wide || true
  kube -n "$EXTERNAL_DNS_NAMESPACE" get deployment \
    "${CLUSTER_NAME}-external-dns" \
    "${SECOND_CLUSTER_NAME}-external-dns" -o wide || true
  kube -n "$OOOI_NAMESPACE" get pods -o wide || true
}

cleanup() {
  require_commands kubectl helm dig make
  kube cluster-info >/dev/null

  local cluster_name resource_name external_dns_name host

  log "Deleting both attachments so finalizers can clean hosted MetalLB and control-plane policies"
  for cluster_name in "$CLUSTER_NAME" "$SECOND_CLUSTER_NAME"; do
    delete_namespaced infraattachment "$cluster_name" "$MANAGEMENT_NAMESPACE"
  done
  for cluster_name in "$CLUSTER_NAME" "$SECOND_CLUSTER_NAME"; do
    if ! wait_deleted "InfraClusterAttachment $cluster_name" "$ATTACHMENT_DELETE_WAIT" infraattachment "$cluster_name" "$MANAGEMENT_NAMESPACE"; then
      log "Forcing removal of the test attachment finalizer after timeout"
      remove_attachment_finalizer "$cluster_name"
      if ! wait_deleted "forced deletion of InfraClusterAttachment $cluster_name" "$GC_WAIT" infraattachment "$cluster_name" "$MANAGEMENT_NAMESPACE"; then
        dump_status
        fail "InfraClusterAttachment $cluster_name did not finish cleanup"
      fi
    fi
    wait_deleted "external proxy Service for $cluster_name" "$GC_WAIT" service "${cluster_name}-proxy-external" "$MANAGEMENT_NAMESPACE" || true
  done

  if [[ "$VERIFY_DNS_CLEANUP" == "true" ]]; then
    local deadline=$((SECONDS + DNS_CLEANUP_WAIT))
    while (( SECONDS < deadline )); do
      if public_dns_absent "oauth.${CLUSTER_DOMAIN}" && \
        public_dns_absent "console-openshift-console.apps.${CLUSTER_DOMAIN}" && \
        public_dns_absent "oauth.${SECOND_CLUSTER_DOMAIN}" && \
        public_dns_absent "console-openshift-console.apps.${SECOND_CLUSTER_DOMAIN}"; then
        log "ExternalDNS OAuth and wildcard records removed"
        break
      fi
      sleep "$POLL_INTERVAL"
    done
    if ! public_dns_absent "oauth.${CLUSTER_DOMAIN}" || \
      ! public_dns_absent "console-openshift-console.apps.${CLUSTER_DOMAIN}" || \
      ! public_dns_absent "oauth.${SECOND_CLUSTER_DOMAIN}" || \
      ! public_dns_absent "console-openshift-console.apps.${SECOND_CLUSTER_DOMAIN}"; then
      log "WARNING: ExternalDNS records still resolve; inspect them before reusing the names"
      for host in \
        "oauth.${CLUSTER_DOMAIN}" \
        "console-openshift-console.apps.${CLUSTER_DOMAIN}" \
        "oauth.${SECOND_CLUSTER_DOMAIN}" \
        "console-openshift-console.apps.${SECOND_CLUSTER_DOMAIN}"; do
        printf '%s answers: %s\n' "$host" "$(public_dns_answers "$host")"
      done
    fi
  fi

  log "Deleting the shared Infra and its generated child CRs"
  delete_namespaced infra "$INFRA_NAME" "$MANAGEMENT_NAMESPACE"
  if ! wait_deleted "Infra $INFRA_NAME" "$GC_WAIT" infra "$INFRA_NAME" "$MANAGEMENT_NAMESPACE"; then
    dump_status
    fail "Infra $INFRA_NAME did not finish deletion"
  fi
  for resource_name in dhcpserver dnsserver proxyserver; do
    if ! wait_deleted "garbage-collected ${INFRA_NAME}-${resource_name}" "$GC_WAIT" "$resource_name" "${INFRA_NAME}-${resource_name}" "$MANAGEMENT_NAMESPACE"; then
      delete_namespaced "$resource_name" "${INFRA_NAME}-${resource_name}" "$MANAGEMENT_NAMESPACE"
    fi
  done
  for cluster_name in "$CLUSTER_NAME" "$SECOND_CLUSTER_NAME"; do
    delete_namespaced networkpolicy allow-infrastructure "${MANAGEMENT_NAMESPACE}-${cluster_name}"
    wait_deleted "control-plane policy for $cluster_name" "$GC_WAIT" networkpolicy allow-infrastructure "${MANAGEMENT_NAMESPACE}-${cluster_name}" || true
  done

  log "Deleting both NodePools and HostedClusters"
  for cluster_name in "$CLUSTER_NAME" "$SECOND_CLUSTER_NAME"; do
    delete_namespaced nodepool "$cluster_name" "$MANAGEMENT_NAMESPACE"
  done
  for cluster_name in "$CLUSTER_NAME" "$SECOND_CLUSTER_NAME"; do
    if ! wait_deleted "NodePool $cluster_name" "$HOSTED_CLUSTER_DELETE_WAIT" nodepool "$cluster_name" "$MANAGEMENT_NAMESPACE"; then
      dump_status
      fail "NodePool $cluster_name did not finish deletion"
    fi
  done
  for cluster_name in "$CLUSTER_NAME" "$SECOND_CLUSTER_NAME"; do
    delete_namespaced hostedcluster "$cluster_name" "$MANAGEMENT_NAMESPACE"
  done
  for cluster_name in "$CLUSTER_NAME" "$SECOND_CLUSTER_NAME"; do
    if ! wait_deleted "HostedCluster $cluster_name" "$HOSTED_CLUSTER_DELETE_WAIT" hostedcluster "$cluster_name" "$MANAGEMENT_NAMESPACE"; then
      dump_status
      fail "HostedCluster $cluster_name did not finish deletion"
    fi
  done

  log "Uninstalling end-user Helm release $SETUP_RELEASE"
  helm_kube uninstall "$SETUP_RELEASE" --namespace "$MANAGEMENT_NAMESPACE" --ignore-not-found --wait --cascade foreground --timeout "$HELM_UNINSTALL_TIMEOUT" || true

  log "Removing dedicated hosted-cluster ExternalDNS resources"
  for cluster_name in "$CLUSTER_NAME" "$SECOND_CLUSTER_NAME"; do
    external_dns_name="${cluster_name}-external-dns"
    delete_namespaced deployment "$external_dns_name" "$EXTERNAL_DNS_NAMESPACE"
    delete_namespaced serviceaccount "$external_dns_name" "$EXTERNAL_DNS_NAMESPACE"
    delete_namespaced secret "${external_dns_name}-kubeconfig" "$EXTERNAL_DNS_NAMESPACE"
    delete_namespaced secret "${external_dns_name}-credentials" "$EXTERNAL_DNS_NAMESPACE"
  done

  log "Removing the Make-installed oooi controller and generated CRDs"
  remove_test_namespace_label "$OOOI_NAMESPACE"
  (
    cd "$REPO_ROOT"
    make undeploy KUBECTL="kubectl --context=$KUBE_CONTEXT" ignore-not-found=true || true
    make uninstall KUBECTL="kubectl --context=$KUBE_CONTEXT" ignore-not-found=true || true
  )
  for crd in \
    infras.hostedcluster.densityops.com \
    dhcpservers.hostedcluster.densityops.com \
    dnsservers.hostedcluster.densityops.com \
    proxyservers.hostedcluster.densityops.com \
    infraclusterattachments.hostedcluster.densityops.com; do
    delete_cluster_scoped crd "$crd"
  done
  if kube get namespace "$OOOI_NAMESPACE" >/dev/null 2>&1; then
    kube delete namespace "$OOOI_NAMESPACE" --ignore-not-found --wait=false || true
    wait_namespace_deleted "$OOOI_NAMESPACE" "$GC_WAIT" || true
  fi

  # etcd-encryption-key-clusters, pullsecret-clusters, sshkey-clusters,
  # aws-access-key, and the shared ExternalDNS CRs are intentionally retained.
  log "Cleanup complete; shared HostedCluster and management ExternalDNS prerequisites were retained"
}

cleanup "$@"
