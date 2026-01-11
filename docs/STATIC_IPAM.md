# Static IPAM Configuration

This document explains how to use static IPAM with the OOOI operator for assigning static IP addresses to infrastructure pods.

## Overview

The OOOI operator now uses the [CNI static IPAM plugin](https://www.cni.dev/plugins/current/ipam/static/) instead of host-local IPAM. This allows for static IP assignment to pods via multus annotations, giving you precise control over which IP address each pod receives.

## Why Static IPAM?

The `host-local` IPAM plugin manages IP address ranges and automatically assigns IPs from a pool, but it doesn't support static IP assignment for individual pods. The `static` IPAM plugin allows you to specify exact IP addresses for each pod through the multus network annotation.

## NetworkAttachmentDefinition Configuration

### Before (host-local IPAM)
```yaml
apiVersion: k8s.cni.cncf.io/v1
kind: NetworkAttachmentDefinition
metadata:
  name: test-vlan-100
  namespace: oooi-system
spec:
  config: |
    {
      "cniVersion": "0.3.1",
      "type": "ipvlan",
      "name": "test-vlan-100",
      "master": "eth0",
      "mode": "l2",
      "ipam": {
        "type": "host-local",
        "subnet": "192.168.100.0/24",
        "gateway": "192.168.100.1",
        "rangeStart": "192.168.100.10",
        "rangeEnd": "192.168.100.250",
        "routes": [
          { "dst": "192.168.100.0/24" }
        ]
      }
    }
```

### After (static IPAM)
```yaml
apiVersion: k8s.cni.cncf.io/v1
kind: NetworkAttachmentDefinition
metadata:
  name: test-vlan-100
  namespace: oooi-system
spec:
  config: |
    {
      "cniVersion": "0.3.1",
      "type": "ipvlan",
      "name": "test-vlan-100",
      "master": "eth0",
      "mode": "l2",
      "ipam": {
        "type": "static"
      }
    }
```

**Key Differences:**
- IPAM type changed from `host-local` to `static`
- Removed `subnet`, `gateway`, `rangeStart`, `rangeEnd`, and `routes` from NAD
- IP configuration now happens per-pod via annotations

## Pod IP Assignment

With static IPAM, the IP address is specified in the pod's multus annotation. The OOOI operator automatically generates this annotation for DHCP, DNS, and Proxy server pods.

### Annotation Format

```yaml
k8s.v1.cni.cncf.io/networks: |-
  [
    {
      "name": "test-vlan-100",
      "namespace": "oooi-system",
      "ips": ["192.168.100.2/24"]
    }
  ]
```

**Components:**
- `name`: Name of the NetworkAttachmentDefinition
- `namespace`: Namespace where the NAD exists
- `ips`: Array of IP addresses with CIDR notation (e.g., `192.168.100.2/24`)

### How OOOI Generates Annotations

The operator uses the `ServerIP` field from each component's spec to generate the multus annotation:

**For DHCP Server:**
```yaml
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: DHCPServer
spec:
  networkConfig:
    serverIP: "192.168.100.2"  # This becomes the static IP
    networkAttachmentName: "test-vlan-100"
    networkAttachmentNamespace: "oooi-system"
```

Generated annotation:
```yaml
annotations:
  k8s.v1.cni.cncf.io/networks: |-
    [
      {
        "name": "test-vlan-100",
        "namespace": "oooi-system",
        "ips": ["192.168.100.2/24"]
      }
    ]
```

**For DNS Server:**
```yaml
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: DNSServer
spec:
  networkConfig:
    serverIP: "192.168.100.3"  # This becomes the static IP
    networkAttachmentName: "test-vlan-100"
    networkAttachmentNamespace: "oooi-system"
```

**For Proxy Server:**
```yaml
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: ProxyServer
spec:
  networkConfig:
    serverIP: "192.168.100.4"  # This becomes the static IP
    networkAttachmentName: "test-vlan-100"
    networkAttachmentNamespace: "oooi-system"
```

## Complete Example

### 1. Create NetworkAttachmentDefinition with Static IPAM

```yaml
apiVersion: k8s.cni.cncf.io/v1
kind: NetworkAttachmentDefinition
metadata:
  name: tenant-vlan-100
  namespace: oooi-system
spec:
  config: |
    {
      "cniVersion": "0.3.1",
      "type": "ipvlan",
      "name": "tenant-vlan-100",
      "master": "eth0",
      "mode": "l2",
      "ipam": {
        "type": "static"
      }
    }
```

### 2. Create Infra Resource

```yaml
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: Infra
metadata:
  name: my-cluster
  namespace: clusters-my-cluster
spec:
  networkConfig:
    cidr: "192.168.100.0/24"
    gateway: "192.168.100.1"
    dnsServers:
      - "8.8.8.8"
      - "8.8.4.4"
    networkAttachmentDefinition: "oooi-system/tenant-vlan-100"
  
  infraComponents:
    dhcp:
      enabled: true
      serverIP: "192.168.100.2"  # Static IP for DHCP server
      rangeStart: "192.168.100.100"
      rangeEnd: "192.168.100.200"
      leaseTime: "1h"
    
    dns:
      enabled: true
      serverIP: "192.168.100.3"  # Static IP for DNS server
      forwardingZones:
        - zone: "cluster.local"
          forwarders:
            - "10.96.0.10"
    
    proxy:
      enabled: true
      serverIP: "192.168.100.4"  # Static IP for Proxy server
```

### 3. Result

The operator creates three pods with static IPs:
- DHCP server: `192.168.100.2/24`
- DNS server: `192.168.100.3/24`
- Proxy server: `192.168.100.4/24`

## Verification

Check that pods have the correct IP addresses:

```bash
# Check DHCP pod
kubectl get pod -n clusters-my-cluster -l app=dhcp-server -o yaml | grep -A 10 "k8s.v1.cni.cncf.io/networks"

# Check DNS pod
kubectl get pod -n clusters-my-cluster -l app=dns-server -o yaml | grep -A 10 "k8s.v1.cni.cncf.io/networks"

# Check Proxy pod
kubectl get pod -n clusters-my-cluster -l app=proxy-server -o yaml | grep -A 10 "k8s.v1.cni.cncf.io/networks"

# Verify actual IP assignment from pod status
kubectl get pod -n clusters-my-cluster -l app=dhcp-server -o jsonpath='{.items[0].metadata.annotations.k8s\.v1\.cni\.cncf\.io/network-status}'
```

## Troubleshooting

### Pod fails to start with IP error

**Symptom:** Pod stuck in `ContainerCreating` with network setup errors

**Common causes:**
1. IP address already in use by another pod
2. Static IPAM plugin not installed on the node
3. Incorrect CIDR notation (missing `/24` suffix)

**Solution:**
```bash
# Check pod events
kubectl describe pod <pod-name> -n <namespace>

# Verify static IPAM plugin exists on nodes
kubectl exec -it <pod-name> -n <namespace> -- ls /opt/cni/bin/static

# Ensure IP is unique and not conflicting
```

### NAD not found

**Symptom:** Error about NetworkAttachmentDefinition not found

**Solution:**
Ensure the NAD exists in the correct namespace:
```bash
kubectl get network-attachment-definitions -n oooi-system
```

## Benefits of Static IPAM

1. **Predictable IPs**: Each infrastructure component gets the same IP across restarts
2. **DNS Integration**: You can create static DNS entries pointing to these IPs
3. **Firewall Rules**: Easier to configure firewall rules with known IPs
4. **Debugging**: Simpler to troubleshoot with consistent IP addresses
5. **Multi-tenant**: Different hosted clusters can use different VLAN/IP spaces

## Migration from host-local to static

If you're migrating from host-local IPAM:

1. **Update NADs**: Remove IP configuration from NADs, set IPAM type to "static"
2. **Define ServerIPs**: Ensure each Infra resource specifies explicit `serverIP` values
3. **Redeploy**: Delete and recreate the Infra resources to apply new network configuration

The operator will automatically generate the correct multus annotations for static IP assignment.
