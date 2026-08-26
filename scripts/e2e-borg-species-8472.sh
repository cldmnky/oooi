#!/usr/bin/env bash
set -Eeuo pipefail

REPO_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
WORK_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/oooi-e2e.XXXXXX")"
trap 'rm -rf "$WORK_ROOT"' EXIT

KUBE_CONTEXT="${KUBE_CONTEXT:-default/api-borg-blahonga-me:6443/kube:admin}"
MANAGEMENT_NAMESPACE="${MANAGEMENT_NAMESPACE:-clusters}"
CLUSTER_NAME="${CLUSTER_NAME:-species-8472}"
SECOND_CLUSTER_NAME="${SECOND_CLUSTER_NAME:-two-of-two}"
INFRA_NAME="${INFRA_NAME:-vlan203}"
BASE_DOMAIN="${BASE_DOMAIN:-clusters.blahonga.me}"
CLUSTER_DOMAIN="${CLUSTER_NAME}.${BASE_DOMAIN}"
SECOND_CLUSTER_DOMAIN="${SECOND_CLUSTER_NAME}.${BASE_DOMAIN}"
CONTROL_PLANE_NAMESPACE="${CONTROL_PLANE_NAMESPACE:-${MANAGEMENT_NAMESPACE}-${CLUSTER_NAME}}"
SECOND_CONTROL_PLANE_NAMESPACE="${SECOND_CONTROL_PLANE_NAMESPACE:-${MANAGEMENT_NAMESPACE}-${SECOND_CLUSTER_NAME}}"

HOME_LAB_ROOT="${HOME_LAB_ROOT:-$REPO_ROOT/../home-lab}"
HOSTED_PREREQUISITES="${HOSTED_PREREQUISITES:-$HOME_LAB_ROOT/demos/multicluster/hosted.yaml}"
END_USER_CHART="${END_USER_CHART:-$REPO_ROOT/scripts/helm/end-user-setup}"

SETUP_RELEASE="${SETUP_RELEASE:-oooi-end-user-setup}"
OOOI_NAMESPACE="${OOOI_NAMESPACE:-oooi-system}"
OOOI_IMAGE="${OOOI_IMAGE:-quay.io/cldmnky/oooi@sha256:19b8edd854c923be7885b64dcc84805c7d53b3b7949b9a80846b21d03906b918}"
BUILD_OOOI="${BUILD_OOOI:-false}"
OOOI_BUILD_REPOSITORY="${OOOI_BUILD_REPOSITORY:-quay.io/cldmnky/oooi-e2e}"
OOOI_BUILD_IMAGE="${OOOI_BUILD_IMAGE:-${OOOI_BUILD_REPOSITORY}:latest}"
OOOI_ROLLOUT_TIMEOUT="${OOOI_ROLLOUT_TIMEOUT:-10m}"
SETUP_HELM_TIMEOUT="${SETUP_HELM_TIMEOUT:-10m}"
SETUP_RESOURCE_WAIT="${SETUP_RESOURCE_WAIT:-120}"

NAD_NAMESPACE="${NAD_NAMESPACE:-default}"
NAD_NAME="${NAD_NAME:-vlan203}"
VLAN_CIDR="${VLAN_CIDR:-10.202.64.0/24}"
VLAN_GATEWAY="${VLAN_GATEWAY:-10.202.64.1}"
DHCP_SERVER_IP="${DHCP_SERVER_IP:-10.202.64.2}"
DNS_SERVER_IP="${DNS_SERVER_IP:-10.202.64.3}"
PROXY_SERVER_IP="${PROXY_SERVER_IP:-10.202.64.4}"
DHCP_RANGE_START="${DHCP_RANGE_START:-10.202.64.200}"
DHCP_RANGE_END="${DHCP_RANGE_END:-10.202.64.254}"
APPS_ADDRESS_POOL="${APPS_ADDRESS_POOL:-vlan203-species-apps}"
APPS_ADDRESS_RANGE="${APPS_ADDRESS_RANGE:-10.202.64.180-10.202.64.190}"
SECOND_APPS_ADDRESS_POOL="${SECOND_APPS_ADDRESS_POOL:-vlan203-two-of-two-apps}"
SECOND_APPS_ADDRESS_RANGE="${SECOND_APPS_ADDRESS_RANGE:-10.202.64.191-10.202.64.199}"
UPSTREAM_DNS_1="${UPSTREAM_DNS_1:-10.201.0.2}"
UPSTREAM_DNS_2="${UPSTREAM_DNS_2:-10.201.0.1}"

MANAGEMENT_METALLB_NAMESPACE="${MANAGEMENT_METALLB_NAMESPACE:-metallb-system}"
MANAGEMENT_METALLB_POOL="${MANAGEMENT_METALLB_POOL:-metallb-pool}"

EXTERNAL_DNS_NAMESPACE="${EXTERNAL_DNS_NAMESPACE:-external-dns-operator}"
EXTERNAL_DNS_IMAGE="${EXTERNAL_DNS_IMAGE:-registry.redhat.io/edo/external-dns-rhel9@sha256:bff2bf48be1d62005dda3b939066fb9d3ba2877799373ec0d1b02feac7728bc9}"
EXTERNAL_DNS_ZONE_ID="${EXTERNAL_DNS_ZONE_ID:-Z0744581GI2T7BTVHI4Y}"
EXTERNAL_DNS_WAIT="${EXTERNAL_DNS_WAIT:-300}"
HOSTED_CLUSTER_WAIT="${HOSTED_CLUSTER_WAIT:-3600}"
ATTACHMENT_WAIT="${ATTACHMENT_WAIT:-1800}"
POLL_INTERVAL="${POLL_INTERVAL:-10}"

PUBLIC_DNS_SERVER="${PUBLIC_DNS_SERVER:-}"
PUBLIC_DNS_WAIT="${PUBLIC_DNS_WAIT:-600}"
VLAN_NETWORK_WAIT="${VLAN_NETWORK_WAIT:-300}"
VERIFY_PUBLIC_DNS="${VERIFY_PUBLIC_DNS:-true}"
VERIFY_VLAN_NETWORK="${VERIFY_VLAN_NETWORK:-true}"
VERIFY_PUBLIC_ENDPOINTS="${VERIFY_PUBLIC_ENDPOINTS:-true}"
REQUIRE_SOURCE_ALIAS="${REQUIRE_SOURCE_ALIAS:-true}"
CURL_CONNECT_TIMEOUT="${CURL_CONNECT_TIMEOUT:-5}"
CURL_MAX_TIME="${CURL_MAX_TIME:-20}"

DHCP_NAME="${INFRA_NAME}-dhcp"
DNS_NAME="${INFRA_NAME}-dns"
PROXY_NAME="${INFRA_NAME}-proxy"
PRIMARY_EXTERNAL_PROXY_SERVICE="${CLUSTER_NAME}-proxy-external"
SECOND_EXTERNAL_PROXY_SERVICE="${SECOND_CLUSTER_NAME}-proxy-external"
INFRA_LABEL_KEY="hostedcluster.densityops.com/network-policy-group"
INFRA_LABEL_VALUE="infrastructure"
INFRA_LABEL_MARKER="oooi.densityops.com/e2e-infrastructure-label"

API_HOST="api.${CLUSTER_DOMAIN}"
API_INT_HOST="api-int.${CLUSTER_DOMAIN}"
OAUTH_HOST="oauth.${CLUSTER_DOMAIN}"
IGNITION_HOST="ignition.${CLUSTER_DOMAIN}"
KONNECTIVITY_HOST="konnectivity.${CLUSTER_DOMAIN}"
CONSOLE_HOST="console-openshift-console.apps.${CLUSTER_DOMAIN}"
SECOND_API_HOST="api.${SECOND_CLUSTER_DOMAIN}"
SECOND_API_INT_HOST="api-int.${SECOND_CLUSTER_DOMAIN}"
SECOND_OAUTH_HOST="oauth.${SECOND_CLUSTER_DOMAIN}"
SECOND_IGNITION_HOST="ignition.${SECOND_CLUSTER_DOMAIN}"
SECOND_KONNECTIVITY_HOST="konnectivity.${SECOND_CLUSTER_DOMAIN}"
SECOND_CONSOLE_HOST="console-openshift-console.apps.${SECOND_CLUSTER_DOMAIN}"

PROXY_CLUSTER_IP=""
PRIMARY_EXTERNAL_PROXY_IP=""
SECOND_EXTERNAL_PROXY_IP=""
APPS_IP=""
SECOND_APPS_IP=""

log() {
  printf '[oooi-e2e] %s\n' "$*"
}

fail() {
  printf '[oooi-e2e] ERROR: %s\n' "$*" >&2
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

decode_base64() {
  if base64 --help 2>&1 | grep -q -- '--decode'; then
    base64 --decode
  else
    base64 -D
  fi
}

preflight() {
  require_commands kubectl helm jq yq curl dig base64 make
  [[ -f "$END_USER_CHART/Chart.yaml" ]] || fail "end-user Helm chart not found: $END_USER_CHART"
  [[ -f "$HOSTED_PREREQUISITES" ]] || fail "HostedCluster prerequisites not found: $HOSTED_PREREQUISITES"
  helm lint "$END_USER_CHART" >/dev/null
  helm template "$SETUP_RELEASE" "$END_USER_CHART" --namespace "$MANAGEMENT_NAMESPACE" >/dev/null
  helm template "$SETUP_RELEASE" "$END_USER_CHART" --namespace "$MANAGEMENT_NAMESPACE" --set externalDNS.enabled=true >/dev/null

  log "Using Kubernetes context: $KUBE_CONTEXT"
  kube cluster-info >/dev/null
  kube get namespace "$MANAGEMENT_NAMESPACE" >/dev/null
  kube get namespace "$EXTERNAL_DNS_NAMESPACE" >/dev/null
  kube -n "$NAD_NAMESPACE" get net-attach-def "$NAD_NAME" >/dev/null
  kube get storageclass lvms-vg1 >/dev/null
  kube -n "$MANAGEMENT_METALLB_NAMESPACE" get ipaddresspool "$MANAGEMENT_METALLB_POOL" >/dev/null
  kube get crd hostedclusters.hypershift.openshift.io nodepools.hypershift.openshift.io machines.cluster.x-k8s.io >/dev/null
  kube get crd externaldnses.externaldns.olm.openshift.io >/dev/null
  kube -n "$EXTERNAL_DNS_NAMESPACE" get secret aws-access-key >/dev/null

  local secret source_name
  for secret in etcd-encryption-key-clusters pullsecret-clusters sshkey-clusters; do
    source_name="$(SECRET_NAME="$secret" yq eval 'select(.kind == "Secret" and .metadata.name == strenv(SECRET_NAME)) | .metadata.name' "$HOSTED_PREREQUISITES")"
    [[ "$source_name" == "$secret" ]] || fail "secret $secret was not found in $HOSTED_PREREQUISITES"
  done
}

ensure_infrastructure_namespace_label() {
  local namespace="$1"
  local existing
  existing="$(kube get namespace "$namespace" -o json | jq -r --arg key "$INFRA_LABEL_KEY" '.metadata.labels[$key] // empty')"
  if [[ -n "$existing" && "$existing" != "$INFRA_LABEL_VALUE" ]]; then
    fail "namespace $namespace has $INFRA_LABEL_KEY=$existing, expected $INFRA_LABEL_VALUE"
  fi
  if [[ -z "$existing" ]]; then
    kube label namespace "$namespace" "$INFRA_LABEL_KEY=$INFRA_LABEL_VALUE" --overwrite >/dev/null
    kube annotate namespace "$namespace" "$INFRA_LABEL_MARKER=true" --overwrite >/dev/null
    log "Labeled namespace $namespace for the HostedCluster control-plane policy"
  fi
}

apply_shared_secrets() {
  log "Applying only the three shared HostedCluster prerequisite Secrets"
  yq eval 'select(.kind == "Secret" and (.metadata.name == "etcd-encryption-key-clusters" or .metadata.name == "pullsecret-clusters" or .metadata.name == "sshkey-clusters"))' "$HOSTED_PREREQUISITES" |
    kube apply -f -
}

build_oooi_if_requested() {
  case "$BUILD_OOOI" in
    false) return ;;
    true) ;;
    *) fail "BUILD_OOOI must be true or false, got: $BUILD_OOOI" ;;
  esac

  if [[ -z "$OOOI_BUILD_REPOSITORY" || -z "$OOOI_BUILD_IMAGE" ]]; then
    fail "OOOI_BUILD_REPOSITORY and OOOI_BUILD_IMAGE must be set when BUILD_OOOI=true"
  fi

  OOOI_IMAGE="$OOOI_BUILD_IMAGE"
  log "Building and pushing oooi with make container-build as $OOOI_IMAGE"
  (
    cd "$REPO_ROOT"
    make container-build IMAGE_TAG_BASE="$OOOI_BUILD_REPOSITORY"
  )
}

install_oooi() {
  build_oooi_if_requested
  log "Generating and installing oooi from the repository with make"
  (
    cd "$REPO_ROOT"
    make manifests generate
    make install KUBECTL="kubectl --context=$KUBE_CONTEXT"
    make deploy KUBECTL="kubectl --context=$KUBE_CONTEXT" IMG="$OOOI_IMAGE"
  )
  for crd in \
    infras.hostedcluster.densityops.com \
    dhcpservers.hostedcluster.densityops.com \
    dnsservers.hostedcluster.densityops.com \
    proxyservers.hostedcluster.densityops.com \
    infraclusterattachments.hostedcluster.densityops.com; do
    kube get crd "$crd" >/dev/null || fail "oooi CRD was not installed: $crd"
  done
  kube -n "$OOOI_NAMESPACE" rollout status deployment/oooi-controller-manager --timeout="$OOOI_ROLLOUT_TIMEOUT"
  ensure_infrastructure_namespace_label "$OOOI_NAMESPACE"
}

install_end_user_setup() {
  log "Installing the end-user Helm chart with two HostedClusters and two attachments"
  helm_kube upgrade --install "$SETUP_RELEASE" "$END_USER_CHART" \
    --namespace "$MANAGEMENT_NAMESPACE" \
    --create-namespace \
    --set-string "infra.components.image=$OOOI_IMAGE" \
    --set-string "externalDNS.namespace=$EXTERNAL_DNS_NAMESPACE" \
    --set-string "externalDNS.image=$EXTERNAL_DNS_IMAGE" \
    --set-string "externalDNS.zoneID=$EXTERNAL_DNS_ZONE_ID" \
    --set externalDNS.enabled=false \
    --wait=watcher \
    --timeout "$SETUP_HELM_TIMEOUT"
}

end_user_resources_present() {
  local kind name
  for kind in hostedcluster nodepool; do
    for name in "$CLUSTER_NAME" "$SECOND_CLUSTER_NAME"; do
      kube -n "$MANAGEMENT_NAMESPACE" get "$kind" "$name" >/dev/null 2>&1 || return 1
    done
  done
  kube -n "$MANAGEMENT_NAMESPACE" get infra "$INFRA_NAME" >/dev/null 2>&1 || return 1
  for name in "$CLUSTER_NAME" "$SECOND_CLUSTER_NAME"; do
    kube -n "$MANAGEMENT_NAMESPACE" get infraattachment "$name" >/dev/null 2>&1 || return 1
  done
}

hosted_cluster_available_for() {
  local cluster_name="$1"
  kube -n "$MANAGEMENT_NAMESPACE" get hostedcluster "$cluster_name" -o json 2>/dev/null |
    jq -e 'any(.status.conditions[]?; .type == "Available" and .status == "True")' >/dev/null
}

hosted_kubeconfig_ready_for() {
  local cluster_name="$1"
  local secret_name data
  secret_name="$(kube -n "$MANAGEMENT_NAMESPACE" get hostedcluster "$cluster_name" -o jsonpath='{.status.kubeconfig.name}' 2>/dev/null || true)"
  [[ -n "$secret_name" ]] || return 1
  data="$(kube -n "$MANAGEMENT_NAMESPACE" get secret "$secret_name" -o jsonpath='{.data.kubeconfig}' 2>/dev/null || true)"
  [[ -n "$data" ]]
}

prepare_hosted_external_dns() {
  local cluster_name="$1"
  local control_plane_namespace="$MANAGEMENT_NAMESPACE-$cluster_name"
  local cluster_domain="$cluster_name.$BASE_DOMAIN"
  local api_host="api.$cluster_domain"
  local external_dns_name="$cluster_name-external-dns"
  local kubeconfig_secret="$external_dns_name-kubeconfig"
  local credentials_secret="$external_dns_name-credentials"
  local secret_name kubeconfig_data raw_kubeconfig internal_kubeconfig
  local credentials_json access_key_b64 secret_key_b64 access_key secret_key
  secret_name="$(kube -n "$MANAGEMENT_NAMESPACE" get hostedcluster "$cluster_name" -o jsonpath='{.status.kubeconfig.name}')"
  kubeconfig_data="$(kube -n "$MANAGEMENT_NAMESPACE" get secret "$secret_name" -o jsonpath='{.data.kubeconfig}')"
  raw_kubeconfig="$WORK_ROOT/$cluster_name-hosted-kubeconfig"
  internal_kubeconfig="$WORK_ROOT/$cluster_name-hosted-kubeconfig-internal"
  printf '%s' "$kubeconfig_data" | decode_base64 > "$raw_kubeconfig"

  HOSTED_API_URL="https://kube-apiserver.${control_plane_namespace}.svc.cluster.local:6443" \
  HOSTED_SERVER_NAME="$api_host" \
    yq eval '.clusters[].cluster.server = strenv(HOSTED_API_URL) | .clusters[].cluster."tls-server-name" = strenv(HOSTED_SERVER_NAME)' \
    "$raw_kubeconfig" > "$internal_kubeconfig"
  grep -Fq "server: $HOSTED_API_URL" "$internal_kubeconfig" || fail "failed to make the hosted kubeconfig use the in-cluster API Service"
  chmod 600 "$raw_kubeconfig" "$internal_kubeconfig"

  credentials_json="$(kube -n "$EXTERNAL_DNS_NAMESPACE" get secret aws-access-key -o json)"
  access_key_b64="$(printf '%s' "$credentials_json" | jq -r '.data.aws_access_key_id // empty')"
  secret_key_b64="$(printf '%s' "$credentials_json" | jq -r '.data.aws_secret_access_key // empty')"
  [[ -n "$access_key_b64" && -n "$secret_key_b64" ]] || fail "management ExternalDNS credentials Secret is incomplete"
  access_key="$(printf '%s' "$access_key_b64" | decode_base64)"
  secret_key="$(printf '%s' "$secret_key_b64" | decode_base64)"
  [[ -n "$access_key" && -n "$secret_key" ]] || fail "management ExternalDNS credentials could not be decoded"

  printf '[default]\naws_access_key_id = %s\naws_secret_access_key = %s\n' "$access_key" "$secret_key" > "$WORK_ROOT/aws-credentials"
  chmod 600 "$WORK_ROOT/aws-credentials"

  kube -n "$EXTERNAL_DNS_NAMESPACE" create secret generic "$kubeconfig_secret" \
    --from-file=config="$internal_kubeconfig" --dry-run=client -o yaml | kube apply -f -
  kube -n "$EXTERNAL_DNS_NAMESPACE" create secret generic "$credentials_secret" \
    --from-file=credentials="$WORK_ROOT/aws-credentials" --dry-run=client -o yaml | kube apply -f -
}

enable_hosted_external_dns() {
  helm_kube upgrade --install "$SETUP_RELEASE" "$END_USER_CHART" \
    --namespace "$MANAGEMENT_NAMESPACE" \
    --create-namespace \
    --set-string "infra.components.image=$OOOI_IMAGE" \
    --set-string "externalDNS.namespace=$EXTERNAL_DNS_NAMESPACE" \
    --set-string "externalDNS.image=$EXTERNAL_DNS_IMAGE" \
    --set-string "externalDNS.zoneID=$EXTERNAL_DNS_ZONE_ID" \
    --set externalDNS.enabled=true \
    --wait=watcher \
    --timeout "$SETUP_HELM_TIMEOUT"
}

external_dns_deployments_ready() {
  local deployment
  for deployment in "${CLUSTER_NAME}-external-dns" "${SECOND_CLUSTER_NAME}-external-dns"; do
    kube -n "$EXTERNAL_DNS_NAMESPACE" get deployment "$deployment" -o json 2>/dev/null |
      jq -e '(.status.readyReplicas // 0) >= (.spec.replicas // 1)' >/dev/null || return 1
  done
}

attachment_ready_for() {
  local cluster_name="$1" cluster_domain="$2"
  kube -n "$MANAGEMENT_NAMESPACE" get infraattachment "$cluster_name" -o json 2>/dev/null |
    jq -e --arg domain "$cluster_domain" '
      (.status.observedGeneration == .metadata.generation) and
      (([.status.conditions[]? | select(.type == "Ready" and .status == "True")] | length) > 0) and
      (.status.domain == $domain) and
      (.status.appsIngressStatus.phase == "Ready") and
      ((.status.appsIngressStatus.externalIP // "") != "")
    ' >/dev/null
}

infra_ready() {
  kube -n "$MANAGEMENT_NAMESPACE" get infra "$INFRA_NAME" -o json 2>/dev/null |
    jq -e '
      (.status.observedGeneration == .metadata.generation) and
      (([.status.conditions[]? | select(.type == "Ready" and .status == "True")] | length) > 0) and
      (.status.componentStatus.dhcpReady == true) and
      (.status.componentStatus.dnsReady == true) and
      (.status.componentStatus.proxyReady == true) and
      ((.status.attachments.total // 0) == 2) and
      ((.status.attachments.ready // 0) == 2)
    ' >/dev/null
}

child_ready() {
  local kind="$1" name="$2"
  kube -n "$MANAGEMENT_NAMESPACE" get "$kind" "$name" -o json 2>/dev/null |
    jq -e '
      (.status.observedGeneration == .metadata.generation) and
      (([.status.conditions[]? | select(.type == "Ready" and .status == "True")] | length) > 0)
    ' >/dev/null
}

service_cluster_ip_ready() {
  local service_name="$1" cluster_ip
  cluster_ip="$(kube -n "$MANAGEMENT_NAMESPACE" get service "$service_name" -o jsonpath='{.spec.clusterIP}' 2>/dev/null || true)"
  [[ -n "$cluster_ip" && "$cluster_ip" != "None" ]]
}

external_proxy_service_ready_for() {
  local service_name="$1"
  kube -n "$MANAGEMENT_NAMESPACE" get service "$service_name" -o json 2>/dev/null |
    jq -e --arg pool "$MANAGEMENT_METALLB_POOL" '
      (.spec.type == "LoadBalancer") and
      (.metadata.annotations["metallb.universe.tf/address-pool"] == $pool) and
      (.metadata.annotations["external-dns.alpha.kubernetes.io/hostname"] != null) and
      (.metadata.labels["external-dns.blahonga.me/publish"] == "yes") and
      ((.status.loadBalancer.ingress[0].ip // "") != "")
    ' >/dev/null
}

proxy_configuration_ready() {
  local minimum_backends=14
  if [[ "$REQUIRE_SOURCE_ALIAS" == "true" ]]; then
    minimum_backends=16
  fi

  if ! kube -n "$MANAGEMENT_NAMESPACE" get proxyserver "$PROXY_NAME" -o json 2>/dev/null |
    jq -e --argjson minimum "$minimum_backends" --arg domain1 "$CLUSTER_DOMAIN" --arg domain2 "$SECOND_CLUSTER_DOMAIN" '
      .spec.backends as $backends |
      (.status.observedGeneration == .metadata.generation) and
      (([.status.conditions[]? | select(.type == "Ready" and .status == "True")] | length) > 0) and
      (.status.backendCount >= $minimum) and
      (all([$domain1, $domain2][]; . as $domain |
        (any($backends[]?; .hostname == ("api." + $domain))) and
        (any($backends[]?; .hostname == ("oauth." + $domain))) and
        (any($backends[]?; .hostname == ("*.apps." + $domain)))
      ))
    ' >/dev/null; then
    return 1
  fi

  if [[ "$REQUIRE_SOURCE_ALIAS" == "true" ]]; then
    kube -n "$MANAGEMENT_NAMESPACE" get proxyserver "$PROXY_NAME" -o json 2>/dev/null |
      jq -e '
        ([.spec.backends[]? | select(
          .hostname == "kubernetes" and
          ((.sourcePrefixRanges // []) | length > 0) and
          all((.sourcePrefixRanges // [])[]; test("^10\\.202\\.64\\.[0-9]{1,3}/32$"))
        )] | length) >= 2
      ' >/dev/null
  fi
}

dns_configuration_ready() {
  kube -n "$MANAGEMENT_NAMESPACE" get dnsserver "$DNS_NAME" -o json 2>/dev/null |
    jq -e --arg domain1 "$CLUSTER_DOMAIN" --arg domain2 "$SECOND_CLUSTER_DOMAIN" --arg dns_ip "$DNS_SERVER_IP" --arg proxy_ip "$PROXY_SERVER_IP" --arg internal_ip "$PROXY_CLUSTER_IP" --arg apps_ip1 "$APPS_IP" --arg apps_ip2 "$SECOND_APPS_IP" '
      .spec.staticEntries as $entries |
      (.status.observedGeneration == .metadata.generation) and
      (([.status.conditions[]? | select(.type == "Ready" and .status == "True")] | length) > 0) and
      (.spec.networkConfig.serverIP == $dns_ip) and
      (.spec.networkConfig.proxyIP == $proxy_ip) and
      (.spec.networkConfig.internalProxyIP == $internal_ip) and
      (all([$domain1, $domain2][]; . as $domain |
        (any($entries[]?; .hostname == ("api." + $domain) and .ip == $proxy_ip)) and
        (any($entries[]?; .hostname == ("oauth." + $domain) and .ip == $proxy_ip))
      )) and
      (any($entries[]?; .hostname == ("*.apps." + $domain1) and .ip == $apps_ip1)) and
      (any($entries[]?; .hostname == ("*.apps." + $domain2) and .ip == $apps_ip2))
    ' >/dev/null
}

wait_for() {
  local description="$1" timeout="$2"
  shift 2
  local deadline=$((SECONDS + timeout))
  while (( SECONDS < deadline )); do
    if "$@"; then
      log "$description"
      return 0
    fi
    sleep "$POLL_INTERVAL"
  done
  printf '[oooi-e2e] timed out waiting for %s\n' "$description" >&2
  return 1
}

dump_diagnostics() {
  log "Diagnostic resource status"
  kube -n "$MANAGEMENT_NAMESPACE" get hostedcluster,nodepool,infra,infraattachment,dhcpserver,dnsserver,proxyserver -o wide || true
  kube -n "$MANAGEMENT_NAMESPACE" get service \
    "$PRIMARY_EXTERNAL_PROXY_SERVICE" \
    "$SECOND_EXTERNAL_PROXY_SERVICE" \
    "$DNS_NAME" \
    "$PROXY_NAME" -o wide || true
  kube -n "$OOOI_NAMESPACE" get pods -o wide || true
  kube -n "$EXTERNAL_DNS_NAMESPACE" get deployment \
    "${CLUSTER_NAME}-external-dns" \
    "${SECOND_CLUSTER_NAME}-external-dns" -o wide || true
  kube -n "$OOOI_NAMESPACE" logs deployment/oooi-controller-manager --tail=100 || true
}

wait_or_die() {
  local description="$1" timeout="$2"
  shift 2
  if ! wait_for "$description" "$timeout" "$@"; then
    dump_diagnostics
    exit 1
  fi
}

rollout_or_die() {
  local name="$1"
  if ! kube -n "$MANAGEMENT_NAMESPACE" rollout status "deployment/$name" --timeout="$OOOI_ROLLOUT_TIMEOUT"; then
    dump_diagnostics
    exit 1
  fi
}

vlan_dns_record_matches() {
  local host="$1" expected="$2" answers
  answers="$(dig +short +time=3 +tries=1 "@$DNS_SERVER_IP" "$host" A 2>/dev/null || true)"
  printf '%s\n' "$answers" | grep -Fxq "$expected"
}

public_dns_record_matches() {
  local host="$1" expected="$2" answers
  if [[ -n "$PUBLIC_DNS_SERVER" ]]; then
    answers="$(dig +short +time=3 +tries=1 "@$PUBLIC_DNS_SERVER" "$host" A 2>/dev/null || true)"
  else
    answers="$(dig +short +time=3 +tries=1 "$host" A 2>/dev/null || true)"
  fi
  printf '%s\n' "$answers" | grep -Fxq "$expected"
}

vlan_dns_ready() {
  vlan_dns_ready_for "$CLUSTER_DOMAIN" "$APPS_IP" || return 1
  vlan_dns_ready_for "$SECOND_CLUSTER_DOMAIN" "$SECOND_APPS_IP"
}

vlan_dns_ready_for() {
  local cluster_domain="$1" apps_ip="$2" host
  for prefix in api api-int oauth ignition konnectivity; do
    host="$prefix.$cluster_domain"
    vlan_dns_record_matches "$host" "$PROXY_SERVER_IP" || return 1
  done
  vlan_dns_record_matches "console-openshift-console.apps.$cluster_domain" "$apps_ip"
}

public_dns_ready() {
  public_dns_ready_for "$CLUSTER_DOMAIN" "$PRIMARY_EXTERNAL_PROXY_IP" "$APPS_IP" || return 1
  public_dns_ready_for "$SECOND_CLUSTER_DOMAIN" "$SECOND_EXTERNAL_PROXY_IP" "$SECOND_APPS_IP"
}

public_dns_ready_for() {
  local cluster_domain="$1" external_proxy_ip="$2" apps_ip="$3"
  public_dns_record_matches "oauth.$cluster_domain" "$external_proxy_ip" &&
    public_dns_record_matches "console-openshift-console.apps.$cluster_domain" "$apps_ip"
}

http_status() {
  curl --silent --show-error --insecure \
    --connect-timeout "$CURL_CONNECT_TIMEOUT" \
    --max-time "$CURL_MAX_TIME" \
    --output /dev/null --write-out '%{http_code}' "$@"
}

vlan_api_endpoint_ready() {
  vlan_api_endpoint_ready_for "$API_HOST" || return 1
  vlan_api_endpoint_ready_for "$SECOND_API_HOST"
}

vlan_api_endpoint_ready_for() {
  local host="$1"
  [[ "$(http_status --resolve "${host}:6443:${PROXY_SERVER_IP}" "https://${host}:6443/version" 2>/dev/null || true)" == "200" ]]
}

vlan_oauth_endpoint_ready() {
  vlan_oauth_endpoint_ready_for "$OAUTH_HOST" || return 1
  vlan_oauth_endpoint_ready_for "$SECOND_OAUTH_HOST"
}

vlan_oauth_endpoint_ready_for() {
  local host="$1"
  [[ "$(http_status --resolve "${host}:443:${PROXY_SERVER_IP}" "https://${host}/oauth/authorize?client_id=openshift-challenging-client&response_type=token" 2>/dev/null || true)" == "401" ]]
}

public_oauth_endpoint_ready() {
  public_oauth_endpoint_ready_for "$OAUTH_HOST" "$PRIMARY_EXTERNAL_PROXY_IP" || return 1
  public_oauth_endpoint_ready_for "$SECOND_OAUTH_HOST" "$SECOND_EXTERNAL_PROXY_IP"
}

public_oauth_endpoint_ready_for() {
  local host="$1" external_proxy_ip="$2"
  [[ "$(http_status --resolve "${host}:443:${external_proxy_ip}" "https://${host}/oauth/authorize?client_id=openshift-challenging-client&response_type=token" 2>/dev/null || true)" == "401" ]]
}

vlan_ignition_endpoint_ready() {
  vlan_ignition_endpoint_ready_for "$IGNITION_HOST" || return 1
  vlan_ignition_endpoint_ready_for "$SECOND_IGNITION_HOST"
}

vlan_ignition_endpoint_ready_for() {
  local host="$1"
  [[ "$(http_status --resolve "${host}:443:${PROXY_SERVER_IP}" "https://${host}/" 2>/dev/null || true)" == "404" ]]
}

vlan_console_endpoint_ready() {
  vlan_console_endpoint_ready_for "$CONSOLE_HOST" "$APPS_IP" || return 1
  vlan_console_endpoint_ready_for "$SECOND_CONSOLE_HOST" "$SECOND_APPS_IP"
}

vlan_console_endpoint_ready_for() {
  local host="$1" apps_ip="$2"
  [[ "$(http_status --resolve "${host}:443:${apps_ip}" "https://${host}/" 2>/dev/null || true)" == "200" ]]
}

main() {
  preflight
  apply_shared_secrets
  install_oooi
  install_end_user_setup

  wait_or_die "end-user HostedClusters, NodePools, Infra, and attachments" "$SETUP_RESOURCE_WAIT" end_user_resources_present
  wait_or_die "$CLUSTER_NAME HostedCluster Available=True" "$HOSTED_CLUSTER_WAIT" hosted_cluster_available_for "$CLUSTER_NAME"
  wait_or_die "$SECOND_CLUSTER_NAME HostedCluster Available=True" "$HOSTED_CLUSTER_WAIT" hosted_cluster_available_for "$SECOND_CLUSTER_NAME"
  wait_or_die "$CLUSTER_NAME HostedCluster kubeconfig Secret" "$HOSTED_CLUSTER_WAIT" hosted_kubeconfig_ready_for "$CLUSTER_NAME"
  wait_or_die "$SECOND_CLUSTER_NAME HostedCluster kubeconfig Secret" "$HOSTED_CLUSTER_WAIT" hosted_kubeconfig_ready_for "$SECOND_CLUSTER_NAME"

  log "Creating hosted-cluster kubeconfig Secrets for dedicated ExternalDNS"
  prepare_hosted_external_dns "$CLUSTER_NAME"
  prepare_hosted_external_dns "$SECOND_CLUSTER_NAME"
  enable_hosted_external_dns
  wait_or_die "dedicated hosted ExternalDNS Deployments" "$EXTERNAL_DNS_WAIT" external_dns_deployments_ready

  wait_or_die "$CLUSTER_NAME InfraClusterAttachment Ready with apps VIP" "$ATTACHMENT_WAIT" attachment_ready_for "$CLUSTER_NAME" "$CLUSTER_DOMAIN"
  wait_or_die "$SECOND_CLUSTER_NAME InfraClusterAttachment Ready with apps VIP" "$ATTACHMENT_WAIT" attachment_ready_for "$SECOND_CLUSTER_NAME" "$SECOND_CLUSTER_DOMAIN"
  wait_or_die "Infra Ready with all components and attachments" "$ATTACHMENT_WAIT" infra_ready
  wait_or_die "$DHCP_NAME Ready" "$ATTACHMENT_WAIT" child_ready dhcpserver "$DHCP_NAME"
  wait_or_die "$DNS_NAME Ready" "$ATTACHMENT_WAIT" child_ready dnsserver "$DNS_NAME"
  wait_or_die "$PROXY_NAME Ready" "$ATTACHMENT_WAIT" child_ready proxyserver "$PROXY_NAME"

  rollout_or_die "$DHCP_NAME"
  rollout_or_die "$DNS_NAME"
  rollout_or_die "$PROXY_NAME"
  wait_or_die "$PROXY_NAME ClusterIP" "$ATTACHMENT_WAIT" service_cluster_ip_ready "$PROXY_NAME"
  PROXY_CLUSTER_IP="$(kube -n "$MANAGEMENT_NAMESPACE" get service "$PROXY_NAME" -o jsonpath='{.spec.clusterIP}')"
  APPS_IP="$(kube -n "$MANAGEMENT_NAMESPACE" get infraattachment "$CLUSTER_NAME" -o jsonpath='{.status.appsIngressStatus.externalIP}')"
  SECOND_APPS_IP="$(kube -n "$MANAGEMENT_NAMESPACE" get infraattachment "$SECOND_CLUSTER_NAME" -o jsonpath='{.status.appsIngressStatus.externalIP}')"
  wait_or_die "$PRIMARY_EXTERNAL_PROXY_SERVICE assigned MetalLB VIP" "$ATTACHMENT_WAIT" external_proxy_service_ready_for "$PRIMARY_EXTERNAL_PROXY_SERVICE"
  wait_or_die "$SECOND_EXTERNAL_PROXY_SERVICE assigned MetalLB VIP" "$ATTACHMENT_WAIT" external_proxy_service_ready_for "$SECOND_EXTERNAL_PROXY_SERVICE"
  PRIMARY_EXTERNAL_PROXY_IP="$(kube -n "$MANAGEMENT_NAMESPACE" get service "$PRIMARY_EXTERNAL_PROXY_SERVICE" -o jsonpath='{.status.loadBalancer.ingress[0].ip}')"
  SECOND_EXTERNAL_PROXY_IP="$(kube -n "$MANAGEMENT_NAMESPACE" get service "$SECOND_EXTERNAL_PROXY_SERVICE" -o jsonpath='{.status.loadBalancer.ingress[0].ip}')"

  [[ "$APPS_IP" =~ ^10\.202\.64\.(18[0-9]|190)$ ]] || fail "apps VIP $APPS_IP is outside $APPS_ADDRESS_RANGE"
  [[ "$SECOND_APPS_IP" =~ ^10\.202\.64\.(19[1-9])$ ]] || fail "apps VIP $SECOND_APPS_IP is outside $SECOND_APPS_ADDRESS_RANGE"
  [[ "$APPS_IP" != "$SECOND_APPS_IP" ]] || fail "both hosted clusters received the same apps VIP: $APPS_IP"
  [[ "$PRIMARY_EXTERNAL_PROXY_IP" =~ ^10\.201\.0\.([2-4][0-9]|50)$ ]] || fail "OAuth external Service VIP $PRIMARY_EXTERNAL_PROXY_IP is outside $MANAGEMENT_METALLB_POOL"
  [[ "$SECOND_EXTERNAL_PROXY_IP" =~ ^10\.201\.0\.([2-4][0-9]|50)$ ]] || fail "OAuth external Service VIP $SECOND_EXTERNAL_PROXY_IP is outside $MANAGEMENT_METALLB_POOL"

  wait_or_die "$PROXY_NAME complete SNI configuration" "$ATTACHMENT_WAIT" proxy_configuration_ready
  wait_or_die "$DNS_NAME static records and split-horizon configuration" "$ATTACHMENT_WAIT" dns_configuration_ready

  if [[ "$VERIFY_PUBLIC_DNS" == "true" ]]; then
    wait_or_die "ExternalDNS OAuth and wildcard apps records" "$PUBLIC_DNS_WAIT" public_dns_ready
  else
    log "Skipping public DNS checks (VERIFY_PUBLIC_DNS=$VERIFY_PUBLIC_DNS)"
  fi

  if [[ "$VERIFY_VLAN_NETWORK" == "true" ]]; then
    wait_or_die "VLAN DNS records" "$VLAN_NETWORK_WAIT" vlan_dns_ready
    wait_or_die "VLAN API /version endpoint" "$VLAN_NETWORK_WAIT" vlan_api_endpoint_ready
    wait_or_die "VLAN OAuth endpoint returns 401" "$VLAN_NETWORK_WAIT" vlan_oauth_endpoint_ready
    wait_or_die "VLAN Ignition endpoint returns 404" "$VLAN_NETWORK_WAIT" vlan_ignition_endpoint_ready
    wait_or_die "VLAN console endpoint returns 200" "$VLAN_NETWORK_WAIT" vlan_console_endpoint_ready
  else
    log "Skipping VLAN DNS and endpoint checks (VERIFY_VLAN_NETWORK=$VERIFY_VLAN_NETWORK)"
  fi

  if [[ "$VERIFY_PUBLIC_ENDPOINTS" == "true" ]]; then
    wait_or_die "public OAuth endpoint returns 401" "$PUBLIC_DNS_WAIT" public_oauth_endpoint_ready
  else
    log "Skipping public endpoint checks (VERIFY_PUBLIC_ENDPOINTS=$VERIFY_PUBLIC_ENDPOINTS)"
  fi

  log "End-to-end test passed"
  log "${CLUSTER_NAME} OAuth public VIP: $PRIMARY_EXTERNAL_PROXY_IP"
  log "${CLUSTER_NAME} apps VLAN/public VIP: $APPS_IP"
  log "${SECOND_CLUSTER_NAME} OAuth public VIP: $SECOND_EXTERNAL_PROXY_IP"
  log "${SECOND_CLUSTER_NAME} apps VLAN/public VIP: $SECOND_APPS_IP"
  log "Hosted API: https://${API_HOST}:6443"
  log "Hosted OAuth: https://${OAUTH_HOST}/"
  log "Hosted console: https://${CONSOLE_HOST}/"
}

main "$@"
