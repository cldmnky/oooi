# DNS Setup for Hosted Control Planes

## Overview

The oooi operator provides a dual-view split-horizon DNS solution for OpenShift Hosted Control Planes (HCP) running on isolated secondary networks (VLANs) with OpenShift Virtualization.

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  Secondary Network (VLAN)         Pod Network               │
│  ┌─────────┐                      ┌─────────────┐           │
│  │   VM    │                      │  Mgmt Pod   │           │
│  └────┬────┘                      └──────┬──────┘           │
│       │ Query: api.cluster.com           │ Query            │
│       ▼                                  ▼                  │
│  ┌─────────────────────────────────────────────┐            │
│  │        DNSServer (CoreDNS)                  │            │
│  │  ┌──────────────┐    ┌───────────────────┐  │            │
│  │  │ Multus View  │    │  Default View     │  │            │
│  │  │ CIDR match   │    │  Pod network      │  │            │
│  │  └──────┬───────┘    └────────┬──────────┘  │            │
│  └─────────┼─────────────────────┼─────────────┘            │
│            │ 192.168.100.10      │ envoy.svc:443            │
│            ▼ (external)          ▼ (internal)               │
│     ┌──────────────┐       ┌─────────────────┐              │
│     │ Envoy Proxy  │       │ Envoy Service   │              │
│     │ (Secondary)  │       │ (ClusterIP)     │              │
│     └──────────────┘       └─────────────────┘              │
└─────────────────────────────────────────────────────────────┘
```

### Key Concepts

**Dual-View DNS**: The DNSServer uses CoreDNS view plugin to provide different DNS resolution based on the source IP address:

1. **Multus View** (Secondary Network/VMs):
   - Matches queries from secondary network CIDR
   - Resolves HCP endpoints to **external proxy IP** on secondary network
   - Enables VMs to access hosted control plane services

2. **Default View** (Pod Network):
   - Matches queries from pod network (all other sources)
   - Resolves HCP endpoints to **internal proxy service** (ClusterIP)
   - Enables management cluster pods to access hosted control plane

## Configuration

### 1. Configure Infra Resource

Add both external and internal proxy configuration to your Infra CR:

```yaml
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: Infra
metadata:
  name: my-cluster
  namespace: clusters
spec:
  networkConfig:
    cidr: "192.168.100.0/24"
    gateway: "192.168.100.1"
    networkAttachmentDefinition: "vlan100"
    dnsServers:
      - "8.8.8.8"
      - "8.8.4.4"
  
  infraComponents:
    # DNS Configuration
    dns:
      enabled: true
      serverIP: "192.168.100.3"
      clusterName: "my-cluster"
      baseDomain: "example.com"
      image: "quay.io/cldmnky/oooi:latest"
    
    # Proxy Configuration with both external and internal
    proxy:
      enabled: true
      serverIP: "192.168.100.10"  # External proxy on secondary network
      internalProxyService: "envoy-internal.clusters.svc.cluster.local"  # Internal proxy for pods
      controlPlaneNamespace: "clusters-my-cluster"
```

**Key Fields**:
- `proxy.serverIP`: External Envoy proxy IP on secondary network (for VMs)
- `proxy.internalProxyService`: Internal proxy service name or ClusterIP (for management pods)

### 2. Deploy the Infrastructure

Apply your Infra CR:

```bash
oc apply -f infra.yaml
```

This creates:
- DHCPServer CR (if enabled)
- DNSServer CR with dual-view configuration
- Deployment running CoreDNS with view plugin
- Service exposing DNS on ClusterIP

### 3. Verify DNSServer Status

Check the DNSServer status to get the Service ClusterIP:

```bash
# Get DNSServer status
oc get dnsserver my-cluster-dns -n clusters -o yaml

# Extract Service ClusterIP
oc get dnsserver my-cluster-dns -n clusters -o jsonpath='{.status.serviceClusterIP}'
# Example output: 172.30.42.135
```

The status includes:
- `serviceName`: Name of the DNS Service
- `serviceClusterIP`: ClusterIP to use for OpenShift DNS operator
- `configMapName`: Name of ConfigMap with Corefile
- `deploymentName`: Name of the DNS Deployment

### 4. Configure OpenShift DNS Operator

To enable management cluster pods to access HCP endpoints, configure the OpenShift DNS operator to forward HCP domain queries to your DNSServer:

```bash
# Edit the DNS operator configuration
oc edit dns.operator/default
```

Add a server entry for your hosted cluster domain:

```yaml
apiVersion: operator.openshift.io/v1
kind: DNS
metadata:
  name: default
spec:
  servers:
  - name: hosted-cluster-dns
    zones:
    - my-cluster.example.com  # Your hosted cluster domain
    forwardPlugin:
      policy: Random
      upstreams:
      - 172.30.42.135:53  # ClusterIP from DNSServer status
```

**Important**: Use the exact `serviceClusterIP` from the DNSServer status.

## How It Works

### CoreDNS View Plugin

The DNSServer generates a Corefile using the CoreDNS view plugin:

```corefile
.:53 {
    # View for secondary network (VMs)
    view multus {
        # Match queries from secondary network CIDR
        expr incidr(client_ip(), '192.168.100.0/24')
        
        # HCP domain resolves to external proxy
        my-cluster.example.com {
            hosts {
                192.168.100.10 api.my-cluster.example.com
                192.168.100.10 api-int.my-cluster.example.com
                192.168.100.10 oauth.my-cluster.example.com
                192.168.100.10 ignition.my-cluster.example.com
                192.168.100.10 konnectivity.my-cluster.example.com
                fallthrough
            }
            forward . 8.8.8.8
        }
    }
    
    # View for pod network (management cluster)
    view default {
        # HCP domain resolves to internal proxy service
        my-cluster.example.com {
            hosts {
                envoy-internal.clusters.svc.cluster.local api.my-cluster.example.com
                envoy-internal.clusters.svc.cluster.local api-int.my-cluster.example.com
                envoy-internal.clusters.svc.cluster.local oauth.my-cluster.example.com
                envoy-internal.clusters.svc.cluster.local ignition.my-cluster.example.com
                envoy-internal.clusters.svc.cluster.local konnectivity.my-cluster.example.com
                fallthrough
            }
            forward . 8.8.8.8
        }
    }
    
    log
    errors
    reload 5s
}
```

### DNS Resolution Flow

**From VM on Secondary Network**:
1. VM queries `api.my-cluster.example.com`
2. Query reaches DNSServer on secondary network IP
3. Source IP matches `192.168.100.0/24` CIDR
4. **Multus view** matches → Returns `192.168.100.10` (external proxy)
5. VM connects to external proxy on secondary network

**From Management Cluster Pod**:
1. Pod queries `api.my-cluster.example.com`
2. OpenShift DNS forwards to DNSServer ClusterIP
3. Source IP is from pod network (doesn't match secondary CIDR)
4. **Default view** matches → Returns internal proxy service name
5. Pod connects to internal proxy via ClusterIP service

## Verification

### Test DNS Resolution

**From a test pod in the management cluster**:

```bash
# Create a test pod
oc run test-dns --image=quay.io/quay/busybox -it --rm -- /bin/sh

# Inside the pod, test DNS resolution
nslookup api.my-cluster.example.com
# Should resolve to internal proxy service or IP

# Test connectivity
wget -O- https://api.my-cluster.example.com:6443/healthz
```

**From a VM on the secondary network**:

```bash
# SSH to VM
ssh core@<vm-ip>

# Test DNS resolution
nslookup api.my-cluster.example.com
# Should resolve to 192.168.100.10 (external proxy)

# Test connectivity
curl -k https://api.my-cluster.example.com:6443/healthz
```

### Check DNSServer Logs

```bash
# View CoreDNS logs
oc logs -n clusters deployment/my-cluster-dns-dns

# Watch for DNS queries
oc logs -n clusters deployment/my-cluster-dns-dns -f | grep api.my-cluster.example.com
```

### Verify Corefile Configuration

```bash
# Check the generated Corefile
oc get configmap my-cluster-dns-dns-config -n clusters -o yaml

# Look for view plugin configuration
oc get configmap my-cluster-dns-dns-config -n clusters -o jsonpath='{.data.Corefile}'
```

## Troubleshooting

### Management Pods Can't Resolve HCP Domain

**Symptom**: Pods get NXDOMAIN or timeout when querying HCP endpoints

**Check**:
1. Verify OpenShift DNS operator configuration:
   ```bash
   oc get dns.operator/default -o yaml
   ```
   
2. Ensure the zones and upstreams are correct

3. Check OpenShift DNS pods for errors:
   ```bash
   oc logs -n openshift-dns ds/dns-default
   ```

4. Test direct query to DNSServer:
   ```bash
   oc run test --image=quay.io/quay/busybox -it --rm -- nslookup api.my-cluster.example.com <service-clusterip>
   ```

### VMs Can't Resolve HCP Domain

**Symptom**: VMs on secondary network can't resolve or get wrong IP

**Check**:
1. Verify DHCP is providing correct DNS server:
   ```bash
   # On VM
   cat /etc/resolv.conf
   # Should show 192.168.100.3 (DNS ServerIP)
   ```

2. Test direct query to DNS server:
   ```bash
   nslookup api.my-cluster.example.com 192.168.100.3
   ```

3. Check DNSServer is bound to secondary network:
   ```bash
   oc get pod -n clusters -l app=dns-server -o yaml | grep "k8s.v1.cni.cncf.io/networks"
   ```

### Wrong Proxy IP Returned

**Symptom**: DNS returns wrong proxy IP for client type

**Check**:
1. Verify view plugin CIDR matching:
   ```bash
   oc get configmap my-cluster-dns-dns-config -n clusters -o jsonpath='{.data.Corefile}' | grep incidr
   ```

2. Ensure secondary network CIDR is correct:
   ```bash
   oc get dnsserver my-cluster-dns -n clusters -o jsonpath='{.spec.networkConfig.secondaryNetworkCIDR}'
   ```

3. Check client source IP in DNS logs:
   ```bash
   oc logs -n clusters deployment/my-cluster-dns-dns | grep "client_ip"
   ```

### Internal Proxy Not Configured

**Symptom**: Default view has no HCP records or only forwards to upstream

**Check**:
1. Verify InternalProxyService is set:
   ```bash
   oc get dnsserver my-cluster-dns -n clusters -o jsonpath='{.spec.networkConfig.internalProxyIP}'
   ```

2. If empty, update Infra CR with `proxy.internalProxyService`

3. Check Corefile for default view hosts entries:
   ```bash
   oc get configmap my-cluster-dns-dns-config -n clusters -o jsonpath='{.data.Corefile}' | grep -A 20 "view default"
   ```

## Advanced Configuration

### Custom DNS Entries

To add custom DNS entries, update the Infra CR or create a DNSServer CR directly with additional staticEntries:

```yaml
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: DNSServer
metadata:
  name: my-cluster-dns
  namespace: clusters
spec:
  networkConfig:
    serverIP: "192.168.100.3"
    proxyIP: "192.168.100.10"
    internalProxyIP: "envoy-internal.clusters.svc.cluster.local"
    secondaryNetworkCIDR: "192.168.100.0/24"
  hostedClusterDomain: "my-cluster.example.com"
  staticEntries:
  - hostname: "api.my-cluster.example.com"
    ip: "192.168.100.10"
  - hostname: "custom.my-cluster.example.com"  # Custom entry
    ip: "192.168.100.10"
  upstreamDNS:
  - "8.8.8.8"
```

### Changing DNS Port

By default, DNS runs on port 53. To use a different port:

```yaml
spec:
  infraComponents:
    dns:
      enabled: true
      serverIP: "192.168.100.3"
      # DNSPort is set automatically to 53, but can be customized in DNSServer CR
```

Note: Port 53 is privileged and requires the container to run as privileged.

### Adjusting Cache and Reload

Modify DNS caching and configuration reload intervals via DNSServer CR:

```yaml
spec:
  cacheTTL: "30s"       # DNS response cache TTL
  reloadInterval: "5s"  # How often to check for Corefile changes
```

## Integration with Other Components

### DHCP Integration

The DHCP server automatically configures VMs to use the DNS server:

```yaml
spec:
  infraComponents:
    dhcp:
      enabled: true
      serverIP: "192.168.100.2"
    dns:
      enabled: true
      serverIP: "192.168.100.3"  # DHCP will advertise this as DNS server
```

### Envoy Proxy Integration

The DNS setup relies on Envoy proxies being configured:

- **External Proxy**: Deployed on secondary network with static IP
- **Internal Proxy**: Service in management cluster for pod access

Both proxies should route traffic to the actual HCP services in the control plane namespace.

## Reference

### Infra CR DNS Fields

| Field | Description | Required | Default |
|-------|-------------|----------|---------|
| `infraComponents.dns.enabled` | Enable DNS server | No | `false` |
| `infraComponents.dns.serverIP` | Static IP for DNS on secondary network | Yes | - |
| `infraComponents.dns.clusterName` | Hosted cluster name | Yes | - |
| `infraComponents.dns.baseDomain` | Base domain for cluster | Yes | - |
| `infraComponents.dns.image` | DNS container image | No | `quay.io/cldmnky/oooi:latest` |
| `infraComponents.proxy.serverIP` | External proxy IP | Yes | - |
| `infraComponents.proxy.internalProxyService` | Internal proxy service | No | - |

### DNSServer CR Fields

| Field | Description | Required | Default |
|-------|-------------|----------|---------|
| `networkConfig.serverIP` | DNS server IP on secondary network | Yes | - |
| `networkConfig.proxyIP` | External proxy IP for multus view | Yes | - |
| `networkConfig.internalProxyIP` | Internal proxy for default view | No | - |
| `networkConfig.secondaryNetworkCIDR` | CIDR for view matching | No | - |
| `hostedClusterDomain` | HCP domain | Yes | - |
| `staticEntries` | DNS A records | Yes | - |
| `upstreamDNS` | Upstream DNS servers | No | `["8.8.8.8"]` |
| `cacheTTL` | DNS cache TTL | No | `"30s"` |
| `reloadInterval` | Config reload interval | No | `"5s"` |

### DNSServer Status Fields

| Field | Description |
|-------|-------------|
| `serviceName` | Name of the DNS Service |
| `serviceClusterIP` | ClusterIP for OpenShift DNS forwarding |
| `configMapName` | Name of ConfigMap with Corefile |
| `deploymentName` | Name of DNS Deployment |
| `conditions` | Status conditions |

## See Also

- [PLAN.md](../PLAN.md) - Overall architecture and design
- [OpenShift DNS Operator Documentation](https://docs.openshift.com/container-platform/latest/networking/dns-operator.html)
- [CoreDNS View Plugin](https://coredns.io/plugins/view/)
- [Red Hat Solution: Custom CoreDNS](https://access.redhat.com/solutions/6616721)
