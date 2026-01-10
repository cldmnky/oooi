# OpenShift Security Context Constraints (SCC) Requirements

## Overview

The oooi operator deploys infrastructure components (DHCP, DNS, Proxy) that require specific Security Context Constraints in OpenShift. This document explains the SCC requirements and how to configure them.

## Why SCCs Matter

OpenShift uses Security Context Constraints to control what pods can do. The default `restricted` and `restricted-v2` SCCs are very restrictive and don't allow:
- Running as root (UID 0)
- Binding to privileged ports (< 1024)
- Using host networking

## Component SCC Requirements

### Proxy Server (Envoy)

**Requirement**: Must bind to privileged ports (443, 6443) on secondary network interface

**Why**: The proxy uses Multus NetworkAttachmentDefinitions to attach to an isolated VLAN. Traffic comes directly to the pod IP on the secondary network, not through a Kubernetes Service. Therefore, the container must bind to the actual port (443, 6443) - Service-based port mapping doesn't work.

**SCC Needed**: `anyuid` or custom SCC

**Options**:

1. **Use `anyuid` SCC (Recommended for Testing)**:
   ```bash
   # Add the service account to anyuid SCC
   oc adm policy add-scc-to-user anyuid \
     -z default \
     -n <namespace>
   ```

2. **Create Custom SCC (Recommended for Production)**:
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
   allowedCapabilities: null
   defaultAddCapabilities: null
   fsGroup:
     type: RunAsAny
   priority: null
   readOnlyRootFilesystem: false
   requiredDropCapabilities:
   - KILL
   - MKNOD
   - SETUID
   - SETGID
   runAsUser:
     type: RunAsAny  # Allows running as root for port binding
   seLinuxContext:
     type: MustRunAs
   supplementalGroups:
     type: RunAsAny
   users: []
   volumes:
   - configMap
   - downwardAPI
   - emptyDir
   - persistentVolumeClaim
   - projected
   - secret
   ```

   Apply and bind:
   ```bash
   oc apply -f oooi-proxy-scc.yaml
   oc adm policy add-scc-to-user oooi-proxy -z default -n <namespace>
   ```

### DHCP Server

**Requirement**: Standard networking

**SCC Needed**: `restricted` (default) - works without changes

### DNS Server

**Requirement**: Standard networking

**SCC Needed**: `restricted` (default) - works without changes

## Alternative Approaches (Future Considerations)

If running as root is not acceptable, consider these alternatives:

### 1. Use Non-Privileged Ports with iptables

Run Envoy on high ports and use iptables in an init container to redirect:
- Container binds to 8443, 16443
- iptables redirects 443 → 8443, 6443 → 16443
- Requires `NET_ADMIN` capability (still needs custom SCC)

### 2. Use HostPort

Use `hostPort` in the pod spec:
- Container binds to high port
- `hostPort` exposes low port on the node
- Requires `hostnetwork` SCC or custom SCC

### 3. Use External Load Balancer

Deploy an external load balancer that:
- Accepts traffic on 443, 6443
- Forwards to Envoy on high ports
- Requires additional infrastructure

## Deployment Workflow

### Step 1: Create Namespace
```bash
oc new-project hosted-clusters
```

### Step 2: Apply SCC (choose one)

**Option A - Use anyuid** (simpler, less secure):
```bash
oc adm policy add-scc-to-user anyuid -z default -n hosted-clusters
```

**Option B - Create custom SCC** (more secure):
```bash
cat <<EOF | oc apply -f -
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
- persistentVolumeClaim
- projected
- secret
EOF

oc adm policy add-scc-to-user oooi-proxy -z default -n hosted-clusters
```

### Step 3: Deploy Operator
```bash
make deploy IMG=quay.io/cldmnky/oooi:latest
```

### Step 4: Verify SCC Assignment
```bash
# Check which SCC the pod is using
oc get pod <proxy-pod-name> -o jsonpath='{.metadata.annotations.openshift\.io/scc}'

# Should output: anyuid or oooi-proxy
```

### Step 5: Troubleshooting

If pods fail to start with errors like:
```
Error: container has runAsNonRoot and image will run as root
```

Check:
1. SCC is applied: `oc describe scc anyuid | grep -A5 Users`
2. Service account has SCC: `oc get sa default -o yaml`
3. Pod has SCC annotation: `oc get pod <name> -o yaml | grep scc`

## Security Considerations

### Why Running as Root is Safe Here

1. **Network Isolation**: The proxy runs on an isolated secondary network (VLAN), not the main pod network
2. **Limited Scope**: Only binds to network ports, doesn't access host resources
3. **Ephemeral**: Pods are stateless and can be recreated
4. **Read-Only Root**: Consider adding `readOnlyRootFilesystem: true` to further restrict

### Best Practices

1. **Use Custom SCC**: Don't use `privileged` or `anyuid` in production
2. **Limit Scope**: Only grant SCC to the specific service account, not cluster-wide
3. **Audit**: Monitor which pods use which SCCs
4. **Drop Capabilities**: Even when running as root, drop unnecessary capabilities:
   ```yaml
   securityContext:
     capabilities:
       drop:
       - ALL
   ```

## Summary

| Component | Ports | SCC Required | Rationale |
|-----------|-------|--------------|-----------|
| Proxy | 443, 6443 | `anyuid` or custom | Direct port binding on secondary network |
| DHCP | Non-privileged | `restricted` (default) | Standard operation |
| DNS | Non-privileged | `restricted` (default) | Standard operation |

For production OpenShift deployments, create a custom SCC that allows `RunAsAny` but restricts other permissions. This provides the minimum privileges needed while maintaining security.
