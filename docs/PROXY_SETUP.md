# Proxy Setup for Hosted Control Planes

## Overview

The oooi operator provides an Envoy-based L4 proxy solution for OpenShift Hosted Control Planes (HCP) running on isolated secondary networks (VLANs) with OpenShift Virtualization. The proxy enables tenant workloads on isolated VLANs to access management cluster services using SNI-based routing.

### Architecture

```
┌───────────────────────────────────────────────────────────────────────────┐
│  Secondary Network (VLAN 100)        Management Cluster Pod Network       │
│                                                                            │
│  ┌─────────────┐                                                          │
│  │  Tenant VM  │                                                          │
│  │ 192.168.100 │                                                          │
│  │    .50      │                                                          │
│  └──────┬──────┘                                                          │
│         │ TLS SNI: api.cluster.example.com                                │
│         │ Dest: 192.168.100.4:6443                                        │
│         ▼                                                                 │
│  ┌─────────────────────────────────────────────────────────┐             │
│  │              Envoy Proxy Pod                            │             │
│  │  ┌──────────────────────┐  ┌────────────────────────┐   │             │
│  │  │  Envoy Container     │  │  Manager Container     │   │             │
│  │  │  - Port 443 (SNI)    │  │  - xDS Control Plane   │   │             │
│  │  │  - Port 6443 (SNI)   │  │  - Dynamic config      │   │             │
│  │  │  - Multus eth1       │  │  - Watches ProxyServer │   │             │
│  │  │    192.168.100.4     │  │    CRD                 │   │             │
│  │  └──────────┬───────────┘  └────────────────────────┘   │             │
│  └─────────────┼──────────────────────────────────────────┘             │
│                │ Route based on SNI hostname                             │
│                ▼                                                          │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │               Management Cluster Services                        │    │
│  │  ┌──────────────────┐  ┌─────────────────┐  ┌────────────────┐  │    │
│  │  │ kube-apiserver   │  │ oauth-openshift │  │ ignition-server│  │    │
│  │  │ svc:6443         │  │ svc:443         │  │ svc:443        │  │    │
│  │  └──────────────────┘  └─────────────────┘  └────────────────┘  │    │
│  │  ┌──────────────────┐  ┌─────────────────────────────────────┐  │    │
│  │  │ konnectivity     │  │       Other HCP Services            │  │    │
│  │  │ svc:8090         │  │                                     │  │    │
│  │  └──────────────────┘  └─────────────────────────────────────┘  │    │
│  └─────────────────────────────────────────────────────────────────┘    │
└───────────────────────────────────────────────────────────────────────────┘
```

### Key Features

- **SNI-Based Routing**: Uses TLS Server Name Indication (SNI) to route traffic to backend services
- **Dynamic Configuration**: xDS-based control plane for real-time configuration updates
- **Multi-Protocol Support**: Handles HTTPS, TLS, and plain TCP traffic
- **Isolated Networks**: Bridges traffic between Multus secondary networks and pod networks
- **OpenShift Compatible**: Works with OpenShift SCCs and Hosted Control Planes
- **High Availability**: Supports multiple replicas with consistent configuration

## Why is the Proxy Needed?

When OpenShift Hosted Control Planes run on isolated VLANs using KubeVirt VMs with `attach-default-network: false`, the VMs cannot directly reach Kubernetes Services on the pod network. The proxy solves this by:

1. **Attaching to Both Networks**: Proxy pod has both Multus secondary interface (VLAN) and default pod network
2. **Privileged Port Binding**: Binds to standard ports (443, 6443) that HCP components expect
3. **SNI Inspection**: Routes TLS traffic based on hostname without terminating TLS
4. **Service Discovery**: Automatically routes to correct backend services in management cluster

## ProxyServer CRD

The `ProxyServer` custom resource defines the proxy configuration:

```yaml
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: ProxyServer
metadata:
  name: mycluster-proxy
  namespace: hosted-clusters
spec:
  # Network configuration (REQUIRED)
  networkConfig:
    # IP address for proxy on secondary network (REQUIRED)
    serverIP: "192.168.100.4"
    
    # Multus NetworkAttachmentDefinition name (defaults to parent namespace if not set)
    networkAttachmentName: "tenant-vlan-100"
    networkAttachmentNamespace: "hosted-clusters"
  
  # Backend service definitions (REQUIRED)
  backends:
    - name: "kube-apiserver-external"
      hostname: "api.mycluster.example.com"
      port: 6443
      targetService: "kube-apiserver"
      targetPort: 6443
      targetNamespace: "clusters-mycluster"
      protocol: "TCP"  # TCP or HTTPS
      timeoutSeconds: 300
    
    - name: "oauth-openshift"
      hostname: "oauth-openshift.apps.mycluster.example.com"
      port: 443
      targetService: "oauth-openshift"
      targetPort: 443
      targetNamespace: "clusters-mycluster"
      protocol: "TCP"
  
  # Optional: Envoy container image (defaults to v1.36.4)
  proxyImage: "envoyproxy/envoy:v1.36.4"
  
  # Optional: Manager container image (defaults to latest)
  managerImage: "quay.io/cldmnky/oooi:latest"
  
  # Optional: Proxy listening port (defaults to 443)
  port: 443
  
  # Optional: xDS control plane port (defaults to 18000)
  xdsPort: 18000
  
  # Optional: Envoy log level (defaults to info)
  # Values: trace, debug, info, warning, error, critical
  logLevel: "info"
```

## Backend Configuration

Each backend in the `backends` array defines a route:

### Required Fields

- **name**: Unique identifier for this backend
- **hostname**: SNI hostname to match (e.g., "api.cluster.example.com")
- **port**: Port the proxy listens on for this backend
- **targetService**: Kubernetes Service name in management cluster
- **targetPort**: Port on the target Service
- **targetNamespace**: Namespace of the target Service

### Optional Fields

- **protocol**: "TCP" (default) or "HTTPS"
- **timeoutSeconds**: Connection timeout (default: 300)

### Common HCP Backends

A typical OpenShift Hosted Control Plane requires these backends:

1. **kube-apiserver (external)**: External API server access
   - Hostname: `api.<cluster>.<domain>`
   - Port: 6443
   
2. **kube-apiserver (internal)**: Internal API server access
   - Hostname: `api-int.<cluster>.<domain>`
   - Port: 6443

3. **oauth-openshift**: OAuth server for authentication
   - Hostname: `oauth-openshift.apps.<cluster>.<domain>`
   - Port: 443

4. **ignition-server**: Machine Config Server for node ignition
   - Hostname: `ignition-server.<cluster>.<domain>`
   - Port: 443

5. **konnectivity-server**: Konnectivity for API server to node communication
   - Hostname: `konnectivity-server.<cluster>.<domain>`
   - Port: 8090 or 443

See [config/samples/hostedcluster_v1alpha1_proxyserver.yaml](../config/samples/hostedcluster_v1alpha1_proxyserver.yaml) for complete examples.

## Deployment Methods

### Method 1: Standalone ProxyServer (Recommended for Testing)

Deploy only the proxy component:

```bash
# Create NetworkAttachmentDefinition
kubectl apply -f - <<EOF
apiVersion: k8s.cni.cncf.io/v1
kind: NetworkAttachmentDefinition
metadata:
  name: tenant-vlan-100
  namespace: hosted-clusters
spec:
  config: |
    {
      "cniVersion": "0.3.1",
      "type": "macvlan",
      "master": "eth1",
      "mode": "bridge",
      "ipam": {
        "type": "static"
      }
    }
EOF

# Create ProxyServer
kubectl apply -f config/samples/hostedcluster_v1alpha1_proxyserver.yaml

# Verify deployment
kubectl get proxyservers -n hosted-clusters
kubectl get pods -n hosted-clusters -l app=proxy-server
```

### Method 2: Via Infra CRD (Recommended for Production)

Deploy complete infrastructure stack (DHCP + DNS + Proxy):

```yaml
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: Infra
metadata:
  name: mycluster-infra
  namespace: hosted-clusters
spec:
  networkConfig:
    cidr: "192.168.100.0/24"
    gateway: "192.168.100.1"
    networkAttachmentDefinition: "tenant-vlan-100"
    dnsServers:
      - "8.8.8.8"
  
  infraComponents:
    dhcp:
      enabled: true
      serverIP: "192.168.100.2"
      rangeStart: "192.168.100.10"
      rangeEnd: "192.168.100.250"
    
    dns:
      enabled: true
      serverIP: "192.168.100.3"
      baseDomain: "example.com"
      clusterName: "mycluster"
    
    proxy:
      enabled: true
      serverIP: "192.168.100.4"
      proxyImage: "envoyproxy/envoy:v1.36.4"
```

The Infra controller will automatically create the ProxyServer resource with proper configuration.

## OpenShift Deployment

**Critical**: The proxy requires privileged port binding (443, 6443) which needs special Security Context Constraints (SCC) in OpenShift.

### Quick Start with anyuid SCC

```bash
# Create namespace
oc new-project hosted-clusters

# Grant anyuid SCC to service account
oc adm policy add-scc-to-user anyuid -z default -n hosted-clusters

# Deploy operator
make deploy IMG=quay.io/cldmnky/oooi:latest

# Create ProxyServer or Infra resource
oc apply -f config/samples/openshift-example.yaml

# Verify SCC is applied
oc get pod <proxy-pod-name> -n hosted-clusters -o jsonpath='{.metadata.annotations.openshift\.io/scc}'
# Should output: anyuid
```

### Production Deployment with Custom SCC

For production, create a custom SCC instead of using anyuid:

```bash
# Create custom SCC
oc apply -f - <<EOF
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
  type: RunAsAny  # Required for binding to ports 443, 6443
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

# Bind SCC to service account
oc adm policy add-scc-to-user oooi-proxy -z default -n hosted-clusters
```

See [openshift-scc-requirements.md](openshift-scc-requirements.md) for detailed SCC documentation.

## How It Works

### xDS Control Plane

The proxy uses Envoy's **xDS (Discovery Service) protocol** for dynamic configuration:

1. **Manager Container**: Runs the xDS control plane
   - Watches ProxyServer CRD for changes
   - Discovers backend Service endpoints using Kubernetes API
   - Generates Envoy configuration (Listeners, Clusters, Routes, Endpoints)
   - Serves configuration via gRPC on port 18000

2. **Envoy Container**: Runs Envoy proxy
   - Connects to manager's xDS server
   - Receives dynamic configuration updates
   - Routes traffic based on SNI hostnames
   - Reports metrics and health status

### SNI-Based Routing

When a TLS connection arrives:

1. **TLS ClientHello**: Client sends TLS handshake with SNI hostname
2. **SNI Inspection**: Envoy reads SNI field without terminating TLS
3. **Route Lookup**: Matches SNI hostname against backend configuration
4. **Forward**: Proxies connection to matched backend Service
5. **Passthrough**: Backend Service terminates TLS, not the proxy

This allows the proxy to route encrypted traffic without access to TLS certificates.

### Network Topology

```
Tenant VM (192.168.100.50)
    ↓ TLS + SNI: api.cluster.com
    ↓ Dest: 192.168.100.4:6443
Proxy Multus Interface (eth1: 192.168.100.4)
    ↓ SNI inspection → match "api.cluster.com"
    ↓ Route to: kube-apiserver.clusters-mycluster.svc:6443
Proxy Pod Network Interface (eth0: 10.244.x.x)
    ↓
Management Cluster Service (10.96.x.x:6443)
    ↓
Backend Pod (10.244.y.y:6443)
```

The proxy acts as a bridge between the isolated VLAN and the pod network.

## Configuration Updates

The proxy supports **hot reloads** without downtime:

```bash
# Update backend configuration
kubectl edit proxyserver mycluster-proxy -n hosted-clusters

# Changes are automatically detected by manager
# Manager generates new Envoy config
# Envoy receives config via xDS and applies it
# No pod restart required
```

Changes that trigger hot reload:
- Adding/removing backends
- Changing backend service names or namespaces
- Updating timeouts or protocols

Changes that require restart:
- Changing `networkConfig.serverIP`
- Changing `port` or `xdsPort`
- Changing container images

## Monitoring and Observability

### Proxy Status

Check ProxyServer status:

```bash
kubectl get proxyserver mycluster-proxy -n hosted-clusters -o yaml

# Look for status conditions:
status:
  conditions:
  - type: Ready
    status: "True"
    reason: DeploymentReady
    message: "Proxy deployment is ready"
  - type: ConfigurationValid
    status: "True"
    reason: ConfigApplied
    message: "Envoy configuration applied successfully"
```

### Pod Logs

**Manager logs** (xDS control plane):
```bash
kubectl logs -n hosted-clusters deployment/proxy-server-mycluster-proxy -c manager

# Look for:
# - "Discovered endpoints for service X"
# - "Generated Envoy configuration"
# - "xDS snapshot pushed to Envoy"
```

**Envoy logs** (proxy):
```bash
kubectl logs -n hosted-clusters deployment/proxy-server-mycluster-proxy -c envoy

# Look for:
# - "upstream cluster X: added"
# - "listener 0.0.0.0:443: ready"
# - "connection to backend X established"
```

Increase Envoy log verbosity:
```yaml
spec:
  logLevel: "debug"  # or "trace" for maximum verbosity
```

### Metrics

Envoy exposes Prometheus metrics on port 9901:

```bash
# Port-forward to access metrics
kubectl port-forward -n hosted-clusters deployment/proxy-server-mycluster-proxy 9901:9901

# Query metrics
curl http://localhost:9901/stats/prometheus

# Key metrics:
# - envoy_cluster_upstream_cx_active: Active connections per backend
# - envoy_cluster_upstream_cx_total: Total connections per backend
# - envoy_listener_downstream_cx_active: Active client connections
# - envoy_listener_downstream_cx_total: Total client connections
```

### Health Checks

Check Envoy admin interface:

```bash
kubectl port-forward -n hosted-clusters deployment/proxy-server-mycluster-proxy 9901:9901

# Cluster status
curl http://localhost:9901/clusters

# Configuration dump
curl http://localhost:9901/config_dump

# Stats
curl http://localhost:9901/stats
```

## Troubleshooting

### Pod Fails to Start

**Symptom**: Pod stuck in `CreateContainerConfigError` or `CrashLoopBackOff`

**Common causes**:

1. **NetworkAttachmentDefinition not found**
   ```bash
   # Check if NAD exists
   kubectl get network-attachment-definitions -n hosted-clusters
   
   # Verify namespace matches
   kubectl get proxyserver mycluster-proxy -o jsonpath='{.spec.networkConfig.networkAttachmentNamespace}'
   ```

2. **OpenShift SCC not applied**
   ```bash
   # Check pod events
   kubectl describe pod <proxy-pod-name> -n hosted-clusters
   
   # Look for: "unable to validate against any security context constraint"
   # Solution: Apply anyuid or custom SCC (see OpenShift Deployment section)
   ```

3. **Image pull errors**
   ```bash
   # Check image pull status
   kubectl describe pod <proxy-pod-name> -n hosted-clusters | grep -A5 "Image"
   
   # Verify image exists
   docker pull envoyproxy/envoy:v1.36.4
   ```

### Connection Refused / Timeout

**Symptom**: Clients cannot connect to proxy, connection times out or refused

**Diagnostics**:

1. **Verify proxy is listening**
   ```bash
   # Exec into proxy pod
   kubectl exec -it -n hosted-clusters deployment/proxy-server-mycluster-proxy -c envoy -- sh
   
   # Check listeners
   netstat -tlnp | grep -E "443|6443"
   # Should show: 0.0.0.0:443 LISTEN, 0.0.0.0:6443 LISTEN
   ```

2. **Verify secondary network IP**
   ```bash
   # Get pod details
   kubectl get pod <proxy-pod-name> -n hosted-clusters -o yaml | grep -A5 "k8s.v1.cni.cncf.io/network-status"
   
   # Should show IP matching spec.networkConfig.serverIP
   ```

3. **Check backend service exists**
   ```bash
   # List backends from ProxyServer
   kubectl get proxyserver mycluster-proxy -o jsonpath='{.spec.backends[*].targetService}'
   
   # Verify each service exists
   kubectl get service -n <targetNamespace> <targetService>
   ```

4. **Test from VLAN network**
   ```bash
   # From a pod/VM on the same VLAN
   curl -vk https://192.168.100.4:443
   # Should get TLS error (expected - need valid SNI)
   
   # Test with proper SNI
   curl -vk --resolve api.cluster.com:443:192.168.100.4 https://api.cluster.com:443
   ```

### Wrong Backend / Routing Error

**Symptom**: Traffic routed to wrong backend or "no healthy upstream"

**Diagnostics**:

1. **Verify SNI hostname configuration**
   ```bash
   # List backend hostnames
   kubectl get proxyserver mycluster-proxy -o jsonpath='{range .spec.backends[*]}{.hostname}{"\n"}{end}'
   
   # Verify client is using correct SNI hostname
   openssl s_client -connect 192.168.100.4:443 -servername api.cluster.com
   ```

2. **Check Envoy configuration**
   ```bash
   kubectl port-forward -n hosted-clusters deployment/proxy-server-mycluster-proxy 9901:9901
   
   # Dump listeners (should show SNI filters)
   curl http://localhost:9901/config_dump | jq '.configs[1].dynamic_listeners'
   
   # Dump clusters (should show backend services)
   curl http://localhost:9901/config_dump | jq '.configs[3].dynamic_active_clusters'
   ```

3. **Check manager logs for xDS errors**
   ```bash
   kubectl logs -n hosted-clusters deployment/proxy-server-mycluster-proxy -c manager --tail=100
   
   # Look for:
   # - "failed to discover endpoints"
   # - "service not found"
   # - "invalid configuration"
   ```

### Performance Issues

**Symptom**: Slow connections, high latency, connection drops

**Diagnostics**:

1. **Check resource usage**
   ```bash
   kubectl top pod -n hosted-clusters -l app=proxy-server
   
   # If CPU/memory is high, adjust resources:
   kubectl edit proxyserver mycluster-proxy
   # Add resources section under deployment template
   ```

2. **Check connection limits**
   ```bash
   # Get active connections
   kubectl exec -n hosted-clusters deployment/proxy-server-mycluster-proxy -c envoy -- sh -c "curl -s localhost:9901/stats | grep cx_active"
   
   # If approaching limits, scale proxy:
   kubectl scale deployment proxy-server-mycluster-proxy --replicas=3 -n hosted-clusters
   ```

3. **Review timeout settings**
   ```bash
   # Check backend timeouts
   kubectl get proxyserver mycluster-proxy -o jsonpath='{range .spec.backends[*]}{.name}: {.timeoutSeconds}s{"\n"}{end}'
   
   # Increase if needed:
   kubectl edit proxyserver mycluster-proxy
   # Set timeoutSeconds: 600 for long-running connections
   ```

### xDS Communication Failure

**Symptom**: Envoy logs show "gRPC config stream closed" or "no healthy upstream"

**Diagnostics**:

1. **Verify manager is running**
   ```bash
   kubectl logs -n hosted-clusters deployment/proxy-server-mycluster-proxy -c manager --tail=50
   
   # Should show: "xDS server listening on :18000"
   ```

2. **Check xDS port connectivity**
   ```bash
   kubectl exec -n hosted-clusters deployment/proxy-server-mycluster-proxy -c envoy -- sh -c "nc -zv localhost 18000"
   # Should show: Connection to localhost 18000 port [tcp/*] succeeded!
   ```

3. **Verify RBAC permissions**
   ```bash
   # Manager needs permissions to read Services and Endpoints
   kubectl auth can-i get services --as=system:serviceaccount:hosted-clusters:default -n clusters-mycluster
   kubectl auth can-i get endpoints --as=system:serviceaccount:hosted-clusters:default -n clusters-mycluster
   
   # If false, check RBAC in config/rbac/role.yaml
   ```

## Security Considerations

### Running as Root

The proxy runs with `runAsNonRoot: false` to bind to privileged ports (443, 6443). This is safe because:

1. **Network Isolation**: Proxy runs on isolated secondary network (VLAN), not pod network
2. **No Host Access**: Uses `allowHostNetwork: false`, `allowHostPorts: false`
3. **Limited Capabilities**: No additional capabilities like CAP_SYS_ADMIN
4. **TLS Passthrough**: Doesn't terminate TLS, can't decrypt traffic
5. **No Data Storage**: Stateless proxy, no persistent volumes

### Alternative: Non-Privileged Ports

If your security policy requires non-root, you can:

1. **Use high ports** (e.g., 8443, 8090)
2. **Update HCP components** to connect to proxy on high ports
3. **Remove runAsNonRoot: false** from security context

However, this requires reconfiguring all HCP components, which may not be practical.

### TLS Passthrough vs Termination

The proxy uses **TLS passthrough** (SNI inspection) rather than TLS termination:

**Advantages**:
- Proxy doesn't need TLS certificates
- End-to-end encryption maintained
- Simpler certificate management
- No certificate rotation needed

**Limitations**:
- Can't inspect traffic content
- Can't add authentication/authorization
- Relies on SNI field in TLS handshake

## Advanced Configuration

### Multiple Replicas for High Availability

Scale proxy for redundancy:

```yaml
apiVersion: hostedcluster.densityops.com/v1alpha1
kind: ProxyServer
metadata:
  name: mycluster-proxy
spec:
  # ... other config ...
  
  # Add replica count (requires updating controller)
  replicas: 3
```

**Note**: Current implementation doesn't support replica configuration in CRD. To scale manually:

```bash
kubectl scale deployment proxy-server-mycluster-proxy --replicas=3 -n hosted-clusters
```

For HA, ensure:
1. Multiple proxy pods share same `serverIP` (requires clustering or load balancing)
2. Or use multiple ProxyServer resources with different IPs and client-side failover

### Custom Resource Limits

Edit deployment to adjust resources:

```bash
kubectl edit deployment proxy-server-mycluster-proxy -n hosted-clusters

# Add under containers:
resources:
  requests:
    memory: "256Mi"
    cpu: "500m"
  limits:
    memory: "1Gi"
    cpu: "2000m"
```

### Custom Envoy Configuration

For advanced Envoy features not exposed via CRD, you can:

1. Fork the operator
2. Modify `internal/controller/proxy_server_controller.go`
3. Add custom Envoy filters or listeners in the xDS configuration generation
4. Rebuild and deploy custom operator image

## Integration with Hosted Control Planes

### Complete HCP Deployment

For a full OpenShift Hosted Control Plane deployment:

1. **Deploy operator**:
   ```bash
   make deploy IMG=quay.io/cldmnky/oooi:latest
   ```

2. **Create Infra resource** (includes DHCP + DNS + Proxy):
   ```bash
   kubectl apply -f config/samples/hostedcluster_v1alpha1_infra.yaml
   ```

3. **Deploy HyperShift control plane**:
   ```bash
   hypershift create cluster kubevirt \
     --name mycluster \
     --namespace clusters \
     --base-domain example.com \
     --node-pool-replicas 3
   ```

4. **Configure HCP to use proxy**:
   - Update HCP Services to use external IPs
   - Configure DNS to point to proxy IP
   - Update ignition config with proxy endpoints

### DNS Integration

The proxy works with the DNSServer component for complete split-horizon DNS:

- **Internal clients** (pod network): DNS returns Service ClusterIP
- **External clients** (VLAN): DNS returns proxy secondary network IP

See [DNS_SETUP.md](DNS_SETUP.md) for DNS configuration.

## References

- **Envoy Documentation**: https://www.envoyproxy.io/docs/envoy/latest/
- **Envoy xDS Protocol**: https://www.envoyproxy.io/docs/envoy/latest/api-docs/xds_protocol
- **OpenShift SCCs**: https://docs.openshift.com/container-platform/latest/authentication/managing-security-context-constraints.html
- **Multus CNI**: https://github.com/k8snetworkplumbingwg/multus-cni
- **HyperShift**: https://hypershift-docs.netlify.app/

## Next Steps

- Review [openshift-scc-requirements.md](openshift-scc-requirements.md) for OpenShift deployment
- Review [DNS_SETUP.md](DNS_SETUP.md) for split-horizon DNS configuration
- See [config/samples/](../config/samples/) for complete examples
- Check [PLAN.md](../PLAN.md) for overall architecture and design decisions
