# OpenShift SCC requirements

The oooi components bind to well-known ports on their Multus interfaces. When
OpenShift integration is enabled, the operator creates component-scoped
RoleBindings to the existing SCCs: `privileged` for DHCP and Proxy, and
`anyuid` for DNS.

## Operator-managed binding

Run the manager with OpenShift integration enabled:

```bash
go run ./main.go manager --enable-openshift=true
```

The deployed manager uses the same flag in its container arguments. Verify the
proxy ServiceAccount and SCC annotation:

```bash
kubectl -n <infra-namespace> get pod <proxy-pod> \
  -o jsonpath='{.spec.serviceAccountName}{"\n"}{.metadata.annotations.openshift\\.io/scc}{"\n"}'
kubectl -n <infra-namespace> get rolebinding <infra>-proxy-privileged-scc
kubectl -n <infra-namespace> get rolebinding <infra>-dhcp-privileged-scc <infra>-dns-anyuid-scc
```

Do not grant `privileged` or `anyuid` cluster-wide. The operator's binding is
limited to the component ServiceAccount and the Infra namespace.

## Custom SCC alternative

If cluster policy does not permit the operator-managed binding, create a custom
SCC with only the capabilities required by the deployment, then bind it to the
proxy ServiceAccount. The exact UID and SELinux settings are cluster policy
decisions; a minimal starting point is:

```yaml
apiVersion: security.openshift.io/v1
kind: SecurityContextConstraints
metadata:
  name: oooi-proxy
allowHostDirVolumePlugin: false
allowHostIPC: false
allowHostNetwork: false
allowHostPID: false
allowHostPorts: false
allowPrivilegeEscalation: true
allowPrivilegedContainer: false
runAsUser:
  type: RunAsAny
seLinuxContext:
  type: MustRunAs
fsGroup:
  type: RunAsAny
supplementalGroups:
  type: RunAsAny
volumes:
- configMap
- downwardAPI
- emptyDir
- projected
- secret
```

Bind it only to the proxy ServiceAccount:

```bash
oc apply -f oooi-proxy-scc.yaml
oc adm policy add-scc-to-user oooi-proxy \
  -z <proxy-service-account> -n <infra-namespace>
```

Check the generated Deployment before selecting a custom SCC. The manager and
Envoy containers share a pod, and the controller may add security settings that
must remain permitted by the SCC.

## Components

| Component | Network requirement | SCC guidance |
|---|---|---|
| DHCP | UDP 67/68 on the secondary network | Use the generated component ServiceAccount and the cluster's allowed SCC |
| DNS | UDP/TCP 53 on the secondary network | Use the generated component ServiceAccount and the cluster's allowed SCC |
| Proxy | TCP 443, 6443, and optional 80 on the secondary network | Operator-managed `privileged` binding or an approved equivalent |

The proxy does not use host networking or host ports. The Multus interface is
attached to the pod through the configured NAD and the service IP is assigned
with static IPAM.

## Troubleshooting

```bash
oc describe pod <proxy-pod> -n <infra-namespace>
oc get events -n <infra-namespace> --sort-by=.lastTimestamp
oc get pod <proxy-pod> -n <infra-namespace> \
  -o jsonpath='{.metadata.annotations.openshift\\.io/scc}{"\n"}'
```

If the pod is rejected by SCC admission, compare the rejection with the
Deployment's `securityContext`, container ports, ServiceAccount, and Multus
annotation. If it starts but cannot bind, check that the VLAN IP is present:

```bash
oc get pod <proxy-pod> -n <infra-namespace> \
  -o jsonpath='{.metadata.annotations.k8s\\.v1\\.cni\\.cncf\\.io/network-status}'
```

Granting an SCC does not configure the physical VLAN, the NAD, routing, or
source-IP anti-spoofing. Those remain network-administrator responsibilities.
