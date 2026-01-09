**Title: Hosted Control Plane Infrastructure Operator: Architectural Design for OpenShift Virtualization with Isolated Secondary Networks**

**Executive Summary**

The paradigm shift toward decoupled Kubernetes control planes—exemplified by Red Hat OpenShift's Hosted Control Planes (formerly HyperShift)—represents a fundamental evolution in cluster management economics and security isolation.1 By separating the control plane (hosted as a workload on a management cluster) from the data plane (running on separate nodes), organizations achieve higher density and stricter security boundaries. However, when this architecture is applied to OpenShift Virtualization, where the data plane consists of Virtual Machines (VMs) running alongside the management infrastructure, network isolation becomes the paramount architectural challenge.1

This report presents a comprehensive architectural design for a "Hosted Control Plane Infrastructure Operator." The primary mandate of this operator is to facilitate the deployment of Hosted Clusters where the virtualized worker nodes communicate exclusively via a secondary network (VLAN), treating it as their primary and default interface.3 This design addresses the specific "air-gapped" requirements of high-security environments, such as telecommunications and financial services, where tenant workloads must be strictly segmented from the infrastructure management plane.

The proposed solution necessitates the orchestration of a dedicated infrastructure suite—comprising a DHCP server, a Split-Horizon CoreDNS server, and an L4 Envoy Proxy—directly onto the secondary VLAN. This document provides an exhaustive technical analysis of the operator’s reconciliation logic, the underlying Linux networking primitives required to solve asymmetric routing challenges (Policy-Based Routing), and the precise configuration of the auxiliary services required to bridge the communication gap between the isolated tenant nodes and the management control plane.4

**1\. Architectural Paradigm: Hosted Control Planes and Network Isolation**

**1.1 The Evolution of Control Plane Decoupling**

In traditional Kubernetes architecture, the control plane and data plane share a fate; they typically reside on the same network substrate and often share physical infrastructure in a way that creates implicit trust zones. The Hosted Control Plane (HCP) model disrupts this by treating the control plane API server, controllers, and etcd as standard pods running within a namespace on a "Management Cluster".2 This model allows for the rapid provisioning of "Hosted Clusters," which comprise only the worker nodes.

When the hosting platform is OpenShift Virtualization, the worker nodes are KubeVirt Virtual Machines. While this offers the flexibility of virtualization, it introduces complex networking requirements. In a standard "connected" deployment, KubeVirt VMs might attach to the default pod network of the management cluster, allowing seamless connectivity to the hosted control plane services.3 However, this "flat" network model violates the security zoning requirements of sophisticated enterprise tenants who demand that their compute resources reside on a dedicated Layer 2 domain (VLAN) with no direct routing to the management infrastructure.1

**1.2 The "Secondary as Default" Requirement**

The specific requirement to utilize a secondary network (VLAN) as the primary interface for the KubeVirt nodes fundamentally alters the cluster bootstrapping process. In this topology:

1.  1.**Network Attachment:** The KubeVirt VMs are configured with attach-default-network: false. They possess no interface on the management cluster's SDN (Software Defined Network).3
    
2.  2.**Default Gateway:** The default route for these VMs must point to the gateway of the secondary VLAN, not the OVN-Kubernetes gateway of the management host.7
    
3.  3.**Isolation Gap:** Because the VMs are on a VLAN and the control plane is on the management pod network (Overlay), there is no intrinsic L3 connectivity between the Kubelet (on the VM) and the API Server (on the Management Cluster).
    
4.  This disconnect necessitates the introduction of an intermediary infrastructure layer. The Infrastructure Operator proposed herein is responsible for deploying and managing this layer, effectively creating a "bridge" that projects the necessary control plane services into the isolated VLAN environment.1
    
5.  **2\. Infrastructure Operator Design Pattern**
    
6.  **2.1 Controller Architecture and Scope**
    
7.  The Hosted Control Plane Infrastructure Operator (HCPIO) is designed as a Kubernetes Controller following the Operator Pattern.9 It watches for the creation and mutation of HostedCluster Custom Resources (CRs) within the management cluster. The operator's scope is critical: while it runs in the management context, it acts upon namespaces created for the hosted control planes (typically named clusters-).10
    
8.  The reconciliation loop for the HCPIO is triggered by events on HostedCluster objects. Upon reconciling a HostedCluster, the operator performs a series of checks to determine if the cluster targets the OpenShift Virtualization platform and if the specific annotation or spec field requesting "secondary network infrastructure" is present.6
    
9.  **2.1.1 Owner References and Lifecycle**
    
10.  To ensure proper garbage collection, all resources created by the HCPIO (Deployments, Services, ConfigMaps, Secrets) must bear an OwnerReference pointing back to the HostedCluster resource. This ensures that when a user deletes a HostedCluster, the Kubernetes garbage collector automatically cascades the deletion to the DHCP server, DNS server, and Envoy proxy associated with that cluster.9
    
11.  **2.2 Permissions and RBAC Strategy**
    
12.  The security model for the operator adheres to the principle of least privilege, but the requirements for network manipulation are significant. The operator requires a ClusterRole that grants permissions to manage resources across the diverse clusters-\* namespaces.14
    
13.  **Table 1: Required RBAC Permissions for HCPIO**
    
14.  **API Group**
    
15.  **Resource**
    
16.  **Verbs**
    
17.  **Justification**
    
18.  hypershift.openshift.io
    
19.  hostedclusters, nodepools
    
20.  get, list, watch
    
21.  Trigger reconciliation based on cluster state.
    
22.  apps
    
23.  deployments, statefulsets
    
24.  create, update, patch, delete
    
25.  Manage lifecycle of infrastructure components.
    
26.  core
    
27.  services, configmaps, secrets
    
28.  create, update, patch, delete
    
29.  Configure DNS zones, DHCP leases, and Envoy listeners.
    
30.  k8s.cni.cncf.io
    
31.  network-attachment-definitions
    
32.  get, list
    
33.  Verify existence of requested VLANs.
    
34.  security.openshift.io
    
35.  securitycontextconstraints
    
36.  use
    
37.  Grant NET\_ADMIN cap to infrastructure pods for routing.
    
38.  The operator must also bind a specific ServiceAccount to the infrastructure pods it creates. This ServiceAccount requires privileges to modify the pod's internal routing table, necessitating usage of the privileged or a custom SCC that allows NET\_ADMIN capabilities.16
    
39.  **3\. Network Topology and Multus Integration**
    
40.  **3.1 Multus CNI Configuration**
    
41.  The foundation of the secondary network connectivity is the Multus CNI plugin, which allows the attachment of multiple network interfaces to a single pod or VM. The operator assumes the existence of a NetworkAttachmentDefinition (NAD) that represents the underlying Layer 2 VLAN.3
    
42.  For KubeVirt workloads, the bridge CNI plugin is the preferred mechanism for secondary interfaces. It connects the VM's interface directly to a Linux bridge on the host, allowing for L2 connectivity that supports features like live migration and avoids the MAC address masquerading issues inherent in macvlan.1
    
43.  Sample NAD Structure:
    
44.  The operator validates that the NAD specified in the HostedCluster spec exists. The configuration typically looks like this:
    

46.  YAML
    

49.  apiVersion: k8s.cni.cncf.io/v1kind: NetworkAttachmentDefinitionmetadata:  name: tenant-vlan-100  namespace: clusters-hostedspec:  config: '{      "cniVersion": "0.4.0",      "type": "bridge",      "bridge": "br100",      "ipam": {}    }'
    
50.  **3.2 The Infrastructure Pod Dual-Homing Challenge**
    
51.  A critical design distinction exists between the **Tenant VMs** and the **Infrastructure Pods** (DHCP, DNS, Envoy).
    

*   ●**Tenant VMs:** Connect _only_ to the secondary VLAN (attach-default-network: false).
    
*   ●**Infrastructure Pods:** Must be **dual-homed**. They require the secondary interface (net1) to serve the tenant VMs, but they also require the default pod network interface (eth0) to communicate with the management cluster's API server and other control plane services.18
    
*   This dual-homing introduces a significant routing asymmetry challenge known as the "Return Traffic Problem".21 By default, a Linux container has a single default gateway, typically pointing to the SDN via eth0. Traffic arriving on net1 (the VLAN) will, by default, have its response routed out via eth0 because the kernel simply follows the default route table. The upstream router on the SDN will likely drop this packet due to Reverse Path Forwarding (RPF) checks or NAT translation failures, as the source IP (from the VLAN) does not match the expected SDN overlay IP.7
    
*   Resolving this routing asymmetry is a prerequisite for the functionality of the DHCP, DNS, and Envoy subsystems.
    
*   **4\. Advanced Traffic Engineering: Policy-Based Routing**
    
*   To enable the infrastructure pods to function correctly on the secondary network, the HCPIO must inject specific configuration logic into these pods to establish **Policy-Based Routing (PBR)**. This ensures that traffic entering the pod via the secondary interface is explicitly routed back out of that same interface.21
    
*   **4.1 The Routing Logic Implementation**
    
*   The operator achieves this by injecting an initContainer or a sidecar script that holds the NET\_ADMIN capability. This script performs the following operations upon pod startup:
    

1.  1.**Identify Interfaces:** Dynamically detect the interface name associated with the Multus attachment (e.g., net1) and its assigned IP address and gateway.
    
2.  2.**Create Custom Routing Table:** Linux supports multiple routing tables. The script creates a new table (e.g., ID 100).
    
3.  3.**Populate Custom Table:**
    
4.  ○Adds a route for the local subnet to table 100.
    
5.  ○Adds a default route via the VLAN gateway to table 100.
    
6.  ○_Crucially:_ ip route add default via dev net1 table 100.
    
7.  4.**Configure IP Rules:** The script adds a rule to the Policy Database (RPDB) instructing the kernel to use table 100 for any packet with a source IP matching the secondary interface's IP.
    
8.  ○ip rule add from /32 table 100 priority 100.
    
9.  **Script Logic:**
    

11.  Bash
    

14.  \# Detected VariablesIFACE="net1"IP\_ADDR=$(ip -4 addr show dev $IFACE | grep inet | awk '{print $2}' | cut -d/ -f1)GATEWAY="192.168.100.1" # Retrieved from specific NAD configuration or env var# Configure Routing Table 100ip route flush table 100ip route add default via $GATEWAY dev $IFACE table 100ip rule add from $IP\_ADDR table 100
    
15.  This configuration effectively "pins" the response traffic to the interface it arrived on, solving the asymmetric routing issue inherent in Multus deployments.21
    
16.  **5\. Subsystem 1: Managed DHCP Service**
    
17.  **5.1 Requirement Analysis**
    
18.  In a standard OpenShift Virtualization deployment, the internal SDN handles IP address management (IPAM). However, when using a bridged secondary network, the VMs behave like physical devices plugged into a switch. They require a DHCP service to obtain an IP address, gateway, and DNS server settings. While an external enterprise DHCP server could be used, the requirement for a self-contained "Hosted Cluster" implies that the operator should provision this service if it doesn't exist, ensuring the cluster is self-sufficient.7
    
19.  **5.2 Deployment Strategy**
    
20.  The operator deploys a standard DHCP server (e.g., isc-dhcp-server or dnsmasq) as a Pod within the hosted control plane namespace.
    

*   ●**StatefulSet vs. Deployment:** A StatefulSet is preferred because the DHCP lease database (dhcpd.leases) requires stable storage. The operator creates a PersistentVolumeClaim (PVC) associated with the pod to ensure that lease information persists across pod restarts. If the pod were to restart without this persistence, it might re-issue IP addresses that are currently in use, causing IP conflicts on the VLAN.24
    
*   **5.3 Configuration Generation**
    
*   The operator generates the dhcpd.conf ConfigMap based on the machineNetwork CIDR specified in the HostedCluster definition.
    
*   **Key Configuration Parameters:**
    
*   ●**Subnet Declaration:** Matches the VLAN CIDR.
    
*   ●**Range:** Defines the pool of IPs available for the tenant VMs. The operator must intelligently calculate this range to avoid the "static" IPs assigned to the infrastructure pods themselves (Gateway, DNS, Envoy).
    
*   ●**Options:**
    
*   ○option routers: Points to the VLAN gateway.
    
*   ○option domain-name-servers: Points to the **CoreDNS Pod IP** (on the secondary network), not the management cluster DNS.4
    
*   ○option domain-name: Sets the cluster domain (e.g., cluster.local).
    
*   The operator utilizes Go text templates to render this configuration dynamically. For example, if the machine network is 192.168.100.0/24:
    
*   ●Reserve .1 for Gateway.
    
*   ●Reserve .2 for CoreDNS (Static assignment via Multus IPAM).
    
*   ●Reserve .3 for Envoy (Static assignment via Multus IPAM).
    
*   ●DHCP Range: .10 to .250.
    
*   The Multus configuration for the DHCP pod itself usually involves a static IP request to ensure it is reachable at a known address, or the use of a Service abstraction if the CNI supports it.20
    
*   **6\. Subsystem 2: Split-Horizon CoreDNS Architecture**
    
*   **6.1 The Split-Horizon Necessity**
    
*   The core connectivity challenge in this architecture is the divergence in service resolution.
    
*   ●**Management View:** The control plane components (e.g., the Cluster Operator) need to resolve services like kube-apiserver to their ClusterIPs (172.30.x.x) on the management SDN.
    
*   ●**Tenant View:** The tenant nodes (KubeVirt VMs) cannot reach 172.30.x.x. They must resolve the **same** DNS names (e.g., api.mycluster.example.com) to the IP address of the **Envoy Proxy** on the secondary VLAN (e.g., 192.168.100.3).26
    
*   This requirement dictates the deployment of a "Split Horizon" DNS server dedicated to the secondary network. It allows the tenant nodes to "see" a different version of the network topology than the management cluster.
    
*   **6.2 CoreDNS Plugin Configuration**
    
*   The operator deploys a CoreDNS Pod configured via a Corefile ConfigMap. The configuration heavily leverages the rewrite and hosts (or file) plugins to intercept queries for specific control plane domains and return the Envoy VIP.28
    
*   **Configuration Components:**
    

1.  1.**hosts Plugin:** Used to define static A-records for the control plane endpoints.
    
2.  ○api.. -> Envoy VLAN IP
    
3.  ○api-int.. -> Envoy VLAN IP
    
4.  ○\*.apps.. -> Envoy VLAN IP (for Ingress traffic)
    
5.  2.**forward Plugin:** All other queries (e.g., for quay.io, google.com, or OS updates) are forwarded to an upstream recursive resolver. This is typically the DNS server of the physical infrastructure, which can be injected via the pod's /etc/resolv.conf.30
    
6.  **Sample Corefile Structure:**
    

10.  .:53 {    errors    health    ready        # Authoritative view for the hosted cluster domain    hosts {        192.168.100.3 api.mycluster.example.com        192.168.100.3 api-int.mycluster.example.com        fallthrough    }    # Forward everything else to the infrastructure DNS    forward. /etc/resolv.conf        cache 30    loop    reload    loadbalance}
    
11.  **6.3 Lifecycle and Updates**
    
12.  The operator monitors the HostedCluster status. If the user changes the base domain or if the Envoy IP changes (which should be rare with static assignment), the operator updates the ConfigMap. CoreDNS's reload plugin automatically detects the ConfigMap change and reloads the configuration without a pod restart, ensuring continuous service availability.30
    
13.  **7\. Subsystem 3: The L4 Envoy Proxy Gateway**
    
14.  **7.1 Role and Protocol Requirements**
    
15.  The Envoy Proxy serves as the critical gateway, exposing the control plane services running on the management cluster's SDN to the isolated tenant VLAN. Since the traffic includes mutual TLS (mTLS) for the API server and potentially other encrypted streams, the proxy functions primarily at Layer 4 (TCP), acting as a transparent conduit.5
    
16.  **Target Services:**
    

*   ●**Kube API Server:** Port 6443.
    
*   ●**OAuth Server:** Port 443.
    
*   ●**Ignition / Machine Config Server:** Port 22623 or 443 (depending on version).
    
*   ●**Konnectivity Server:** Port 8132/TCP (tunnel).33
    
*   **7.2 Listener Configuration**
    
*   The operator generates an envoy.yaml configuration that defines **Listeners** for each of these ports.
    
*   Binding Strategy:
    
*   Standard Envoy configuration typically binds to specific IPs. However, in a Multus environment where the secondary IP might be dynamic or subject to change, binding to 0.0.0.0 (all interfaces) is the most robust approach. The Policy-Based Routing (PBR) described in Section 4 ensures that traffic arriving on the secondary interface is handled correctly.35
    
*   **Envoy Config Fragment:**
    

*   YAML
    

*   listeners:- name: api\_server\_listener  address:    socket\_address:      address: 0.0.0.0      port\_value: 6443  filter\_chains:  - filters:    - name: envoy.filters.network.tcp\_proxy      typed\_config:        "@type": type.googleapis.com/envoy.extensions.filters.network.tcp\_proxy.v3.TcpProxy        stat\_prefix: api\_server        cluster: kube\_apiserver\_cluster
    
*   **7.3 Cluster Resolution: LOGICAL\_DNS**
    
*   The Upstream Clusters in Envoy must point to the Kubernetes Services in the management namespace.
    
*   ●**Constraint:** Service IPs (ClusterIPs) in Kubernetes are virtual IPs managed by kube-proxy/OVN. They are stable but not guaranteed to be static forever.
    
*   ●**Resolution:** Envoy should use the LOGICAL\_DNS discovery type. This tells Envoy to resolve the hostname (e.g., kube-apiserver.clusters-mycluster.svc) to an IP address. Since the Envoy pod is dual-homed and has access to the management cluster's DNS (via eth0), it can resolve these internal Service names to their ClusterIPs.37
    
*   This configuration decouples the proxy from the specific IP addresses of the control plane pods, relying instead on the stable Service abstraction provided by Kubernetes.
    
*   **Envoy Cluster Config:**
    

*   YAML
    

*   clusters:- name: kube\_apiserver\_cluster  connect\_timeout: 5s  type: LOGICAL\_DNS  # Critical: Use IPv4 only if the internal SDN is IPv4  dns\_lookup\_family: V4\_ONLY  load\_assignment:    cluster\_name: kube\_apiserver\_cluster    endpoints:    - lb\_endpoints:      - endpoint:          address:            socket\_address:              address: kube-apiserver.clusters-mycluster.svc              port\_value: 6443
    
*   This design leverages the management cluster's internal DNS to bridge the gap between the overlay network and the VLAN.39
    
*   **7.4 Hot Restart and Config Management**
    
*   The operator manages the envoy.yaml via a ConfigMap. If the configuration needs to change (e.g., adding a new listener for a new service), simply updating the ConfigMap is not enough; Envoy must be signaled to reload.
    
*   ●**Operator Pattern:** The simplest robust pattern for the infrastructure operator is to perform a **Rolling Update** of the Envoy Deployment when the ConfigMap hash changes. While Envoy supports "Hot Restart" via shared memory, implementing this in Kubernetes often requires complex process management wrappers. A Rolling Update ensures a new pod (with the new config) is ready before the old one is terminated, providing zero-downtime availability for the service.41
    
*   **8\. Operational Workflows and Lifecycle**
    
*   **8.1 Provisioning Sequence**
    
*   The HCPIO orchestrates a strict dependency chain to ensuring the Hosted Cluster boots successfully.
    

1.  1.**Detection:** Operator detects HostedCluster with platform: kubevirt and secondary network annotations.
    
2.  2.**IP Allocation:** Operator determines the IP addresses for Gateway, DNS, and Envoy (either statically defined in the CR or calculated from the CIDR).
    
3.  3.**Infrastructure Deploy:**
    
4.  ○Create ConfigMap for DHCP (dhcpd.conf).
    
5.  ○Create ConfigMap for CoreDNS (Corefile).
    
6.  ○Create ConfigMap for Envoy (envoy.yaml).
    
7.  ○Deploy Deployment for CoreDNS and Envoy.
    
8.  ○Deploy StatefulSet for DHCP.
    
9.  4.**Wait for Ready:** The operator polls the status of these workloads. It checks the Ready condition of the pods and, critically, verifies that the Multus interfaces are successfully attached (checking k8s.v1.cni.cncf.io/network-status annotations).18
    
10.  5.**Status Reporting:** Once infrastructure is ready, the operator updates the HostedCluster status (or a specific condition like InfrastructureReady=True). This signals the HyperShift operator to proceed with booting the worker nodes.
    
11.  **8.2 The Node Bootstrap Flow**
    
12.  Understanding the node bootstrap flow confirms the validity of the design:
    
13.  1.**Boot:** The KubeVirt VM boots. It has only one NIC (net1 mapped to eth0 inside the guest).
    
14.  2.**DHCP:** The VM requests an IP. The **HCPIO-managed DHCP** responds with an IP, the VLAN Gateway, and the **HCPIO-managed CoreDNS IP**.
    
15.  3.**Ignition Request:** The VM attempts to fetch its configuration from https://api-int.mycluster.example.com:22623.
    
16.  4.**DNS Resolution:** The VM queries the **HCPIO-managed CoreDNS**. The CoreDNS rewrites this name to the **Envoy Proxy IP**.
    
17.  5.**Connection:** The VM opens a TCP connection to the **Envoy Proxy IP**.
    
18.  6.**Proxy:** Envoy accepts the connection (on the VLAN interface) and proxies it to kube-apiserver (on the Management SDN) via eth0.
    
19.  7.**Success:** The API server responds, the VM receives its config, and the node joins the cluster.
    
20.  **9\. Security and Compliance Considerations**
    
21.  **9.1 Network Security Zones**
    
22.  The architecture strictly enforces the "Red/Black" separation model.
    

*   ●**Red Zone (Management):** Contains the control plane secrets, etcd data, and operator logic. Accessible only via the management SDN.
    
*   ●**Black Zone (Tenant VLAN):** Contains the untrusted tenant workloads.
    
*   ●**The Air Gap:** The only bridge between these zones is the Envoy Proxy. By operating at Layer 4, the Envoy proxy prevents direct IP routing. A tenant workload cannot scan the management network; it can only connect to the specific ports opened on the Envoy listener.
    
*   **9.2 Security Context Constraints (SCC)**
    
*   The infrastructure pods require elevated privileges to manipulate the network stack (Policy-Based Routing).
    
*   ●**Requirement:** NET\_ADMIN capability.
    
*   ●**Implementation:** The operator creates a custom SCC or binds the ServiceAccount to the privileged SCC. This permission should be strictly scoped to the clusters-\* namespaces and not available to tenant workloads.16
    
*   **9.3 TLS and Encryption**
    
*   ●**API Server:** Traffic between the tenant node and the API server is mTLS encrypted. Envoy operates in **Passthrough** mode (using envoy.filters.network.tcp\_proxy), meaning it does not terminate the TLS. This preserves the end-to-end encryption chain; the Envoy proxy cannot inspect the traffic, maintaining the confidentiality of the tenant's data.44
    
*   ●**Ignition:** Ignition configs contain sensitive data (node certificates). This traffic is also HTTPS, and Envoy proxies it essentially as an opaque TCP stream.
    
*   **10\. Conclusion**
    
*   The design of the Hosted Control Plane Infrastructure Operator for OpenShift Virtualization addresses a sophisticated set of requirements at the intersection of cloud-native orchestration and traditional network segmentation. By leveraging the Operator Pattern to orchestrate a suite of infrastructure services—DHCP, Split-Horizon DNS, and an L4 Envoy Gateway—the architecture successfully bridges the divide between the decoupled control plane and the isolated tenant data plane.
    
*   The critical innovation in this design is the resolution of the "Secondary as Default" routing asymmetry via automated Policy-Based Routing injection. This mechanism transforms the standard Kubernetes multi-interface capability (Multus) into a robust, enterprise-grade isolation layer. This solution empowers organizations to deploy fully managed, isolated Kubernetes clusters on existing virtualization infrastructure without compromising on security boundaries or operational agility.
    
*   **11\. References**
    
*   1 OpenShift Hosted Control Planes KubeVirt Networking.
    
*   3 Hypershift NodePool platform KubeVirt additionalNetworks attachDefaultNetwork false.
    
*   4 OKD Hosted Control Planes Deploy Virt.
    
*   1 Hosted Control Plane - KubeVirt Networking Guide.
    
*   7 Red Hat Solutions: OCP Virt Secondary Network Default Gateway.
    
*   13 HyperDHCP Operator Architecture and Controller Logic.
    
*   26 CoreDNS Debugging and Plugin Configuration.
    
*   5 Hypershift HostedControlPlane Service Exposure.
    
*   15 OpenShift Operator Framework Administrator Tasks.
    
*   44 Envoy Proxy Static Configuration Guide.
    
*   28 CoreDNS Rewrite Plugin Documentation.
    
*   9 Kubernetes Operator Pattern Concepts.
    
*   21 Kubernetes Multus CNI Routing Issues.
    
*   21 Envoy Listener Binding and Multus Routing.
    
*   45 Envoy Proxy Listener Architecture.
    
*   22 Red Hat OpenStack on OpenShift Dynamic Routing.
    
*   38 Explanation of Envoy Logical DNS Clusters.
    
*   **Works cited**
    

1.  1.Hosted Control Plane - KubeVirt Networking - OpenShift Examples, accessed January 9, 2026, [https://examples.openshift.pub/cluster-installation/hosted-control-plane/kubevirt-networking/](https://examples.openshift.pub/cluster-installation/hosted-control-plane/kubevirt-networking/)
    
2.  2.Hosted control planes | OpenShift Container Platform | 4.18 - Red Hat Documentation, accessed January 9, 2026, [https://docs.redhat.com/en/documentation/openshift\_container\_platform/4.18/html/hosted\_control\_planes/hosted-control-planes-overview](https://docs.redhat.com/en/documentation/openshift_container_platform/4.18/html/hosted_control_planes/hosted-control-planes-overview)
    
3.  3.Configuring Network - HyperShift, accessed January 9, 2026, [https://hypershift-docs.netlify.app/how-to/kubevirt/configuring-network/](https://hypershift-docs.netlify.app/how-to/kubevirt/configuring-network/)
    
4.  4.Deploying hosted control planes on OpenShift Virtualization - OKD Documentation, accessed January 9, 2026, [https://docs.okd.io/4.18/hosted\_control\_planes/hcp-deploy/hcp-deploy-virt.html](https://docs.okd.io/4.18/hosted_control_planes/hcp-deploy/hcp-deploy-virt.html)
    
5.  5.Exposing the Hosted Control Plane Services - HyperShift, accessed January 9, 2026, [https://hypershift.pages.dev/how-to/common/exposing-services-from-hcp/](https://hypershift.pages.dev/how-to/common/exposing-services-from-hcp/)
    
6.  6.Chapter 1. Hosted control planes overview - Red Hat Documentation, accessed January 9, 2026, [https://docs.redhat.com/en/documentation/openshift\_container\_platform/4.14/html/hosted\_control\_planes/hosted-control-planes-overview](https://docs.redhat.com/en/documentation/openshift_container_platform/4.14/html/hosted_control_planes/hosted-control-planes-overview)
    
7.  7.How to make the secondary interface as a default network in OpenShift Virtualization VM?, accessed January 9, 2026, [https://access.redhat.com/solutions/7053229](https://access.redhat.com/solutions/7053229)
    
8.  8.How to Attach a Secondary Network Interface to VMs in OpenShift without a bond, accessed January 9, 2026, [https://surajblog.medium.com/how-to-attach-a-secondary-network-interface-to-vms-in-openshift-d28c0c72f402](https://surajblog.medium.com/how-to-attach-a-secondary-network-interface-to-vms-in-openshift-d28c0c72f402)
    
9.  9.Operator pattern - Kubernetes, accessed January 9, 2026, [https://kubernetes.io/docs/concepts/extend-kubernetes/operator/](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
    
10.  10.Hosted control planes overview - OKD Documentation, accessed January 9, 2026, [https://docs.okd.io/latest/hosted\_control\_planes/index.html](https://docs.okd.io/latest/hosted_control_planes/index.html)
    
11.  11.Hosted control planes | OpenShift Container Platform | 4.19 - Red Hat Documentation, accessed January 9, 2026, [https://docs.redhat.com/en/documentation/openshift\_container\_platform/4.19/html-single/hosted\_control\_planes/index](https://docs.redhat.com/en/documentation/openshift_container_platform/4.19/html-single/hosted_control_planes/index)
    
12.  12.Effortlessly And Efficiently Provision OpenShift Clusters With OpenShift Virtualization - Red Hat, accessed January 9, 2026, [https://www.redhat.com/en/blog/effortlessly-and-efficiently-provision-openshift-clusters-with-openshift-virtualization](https://www.redhat.com/en/blog/effortlessly-and-efficiently-provision-openshift-clusters-with-openshift-virtualization)
    
13.  13.Controller architecture - HyperShift, accessed January 9, 2026, [https://hypershift.pages.dev/reference/controller-architecture/](https://hypershift.pages.dev/reference/controller-architecture/)
    
14.  14.Understanding OpenShift Role-Based Access Control (RBAC) - hoop.dev, accessed January 9, 2026, [https://hoop.dev/blog/understanding-openshift-role-based-access-control-rbac/](https://hoop.dev/blog/understanding-openshift-role-based-access-control-rbac/)
    
15.  15.Chapter 4. Administrator tasks | Operators | OpenShift Container Platform | 4.18 | Red Hat Documentation, accessed January 9, 2026, [https://docs.redhat.com/en/documentation/openshift\_container\_platform/4.18/html/operators/administrator-tasks](https://docs.redhat.com/en/documentation/openshift_container_platform/4.18/html/operators/administrator-tasks)
    
16.  16.Chapter 8. Using RBAC to define and apply permissions | Authentication and authorization | OpenShift Container Platform | 4.18 | Red Hat Documentation, accessed January 9, 2026, [https://docs.redhat.com/en/documentation/openshift\_container\_platform/4.18/html/authentication\_and\_authorization/using-rbac](https://docs.redhat.com/en/documentation/openshift_container_platform/4.18/html/authentication_and_authorization/using-rbac)
    
17.  17.How to keep Pod network route when specifying a default route? · Issue #847 · k8snetworkplumbingwg/multus-cni - GitHub, accessed January 9, 2026, [https://github.com/k8snetworkplumbingwg/multus-cni/issues/847](https://github.com/k8snetworkplumbingwg/multus-cni/issues/847)
    
18.  18.Chapter 22. Multiple networks | Networking | OpenShift Container Platform | 4.14 | Red Hat Documentation, accessed January 9, 2026, [https://docs.redhat.com/en/documentation/openshift\_container\_platform/4.14/html/networking/multiple-networks](https://docs.redhat.com/en/documentation/openshift_container_platform/4.14/html/networking/multiple-networks)
    
19.  19.Chapter 5. Multiple networks | Networking | Red Hat build of MicroShift | 4.16, accessed January 9, 2026, [https://docs.redhat.com/en/documentation/red\_hat\_build\_of\_microshift/4.16/html/networking/multiple-networks](https://docs.redhat.com/en/documentation/red_hat_build_of_microshift/4.16/html/networking/multiple-networks)
    
20.  20.How to Use Kubernetes Services on Secondary Networks with Multus CNI - Red Hat, accessed January 9, 2026, [https://www.redhat.com/en/blog/how-to-use-kubernetes-services-on-secondary-networks-with-multus-cni](https://www.redhat.com/en/blog/how-to-use-kubernetes-services-on-secondary-networks-with-multus-cni)
    
21.  21.kubernetes Multus CNI causing routing issue on pod networking - Reddit, accessed January 9, 2026, [https://www.reddit.com/r/kubernetes/comments/1kzkfk9/kubernetes\_multus\_cni\_causing\_routing\_issue\_on/](https://www.reddit.com/r/kubernetes/comments/1kzkfk9/kubernetes_multus_cni_causing_routing_issue_on/)
    
22.  22.Deploying a dynamic routing environment | Red Hat OpenStack Services on OpenShift, accessed January 9, 2026, [https://docs.redhat.com/it/documentation/red\_hat\_openstack\_services\_on\_openshift/18.0/html-single/deploying\_a\_dynamic\_routing\_environment/index](https://docs.redhat.com/it/documentation/red_hat_openstack_services_on_openshift/18.0/html-single/deploying_a_dynamic_routing_environment/index)
    
23.  23.Chapter 15. Troubleshooting hosted control planes - Red Hat Documentation, accessed January 9, 2026, [https://docs.redhat.com/en/documentation/openshift\_container\_platform/4.19/html/hosted\_control\_planes/troubleshooting-hosted-control-planes](https://docs.redhat.com/en/documentation/openshift_container_platform/4.19/html/hosted_control_planes/troubleshooting-hosted-control-planes)
    
24.  24.Managing hosted control planes on OpenShift Virtualization - OKD Documentation, accessed January 9, 2026, [https://docs.okd.io/latest/hosted\_control\_planes/hcp-manage/hcp-manage-virt.html](https://docs.okd.io/latest/hosted_control_planes/hcp-manage/hcp-manage-virt.html)
    
25.  25.Kubernetes service for multus interface · Issue #466 - GitHub, accessed January 9, 2026, [https://github.com/k8snetworkplumbingwg/multus-cni/issues/466](https://github.com/k8snetworkplumbingwg/multus-cni/issues/466)
    
26.  26.How to Debug DNS Resolution Issues in Kubernetes with CoreDNS - OneUptime, accessed January 9, 2026, [https://oneuptime.com/blog/post/2026-01-08-kubernetes-coredns-dns-resolution-debugging/view](https://oneuptime.com/blog/post/2026-01-08-kubernetes-coredns-dns-resolution-debugging/view)
    
27.  27.Custom DNS Entries For Kubernetes - CoreDNS, accessed January 9, 2026, [https://coredns.io/2017/05/08/custom-dns-entries-for-kubernetes/](https://coredns.io/2017/05/08/custom-dns-entries-for-kubernetes/)
    
28.  28.rewrite - CoreDNS, accessed January 9, 2026, [https://coredns.io/plugins/rewrite/](https://coredns.io/plugins/rewrite/)
    
29.  29.hosts - CoreDNS, accessed January 9, 2026, [https://coredns.io/plugins/hosts/](https://coredns.io/plugins/hosts/)
    
30.  30.Customizing DNS Service - Kubernetes, accessed January 9, 2026, [https://kubernetes.io/docs/tasks/administer-cluster/dns-custom-nameservers/](https://kubernetes.io/docs/tasks/administer-cluster/dns-custom-nameservers/)
    
31.  31.reload - CoreDNS, accessed January 9, 2026, [https://coredns.io/plugins/reload/](https://coredns.io/plugins/reload/)
    
32.  32.Chapter 14. Networking for hosted control planes - Red Hat Documentation, accessed January 9, 2026, [https://docs.redhat.com/en/documentation/openshift\_container\_platform/4.18/html/hosted\_control\_planes/networking-for-hosted-control-planes](https://docs.redhat.com/en/documentation/openshift_container_platform/4.18/html/hosted_control_planes/networking-for-hosted-control-planes)
    
33.  33.Chapter 4. Deploying hosted control planes - Red Hat Documentation, accessed January 9, 2026, [https://docs.redhat.com/en/documentation/openshift\_container\_platform/4.18/html/hosted\_control\_planes/deploying-hosted-control-planes](https://docs.redhat.com/en/documentation/openshift_container_platform/4.18/html/hosted_control_planes/deploying-hosted-control-planes)
    
34.  34.Konnectivity in HyperShift, accessed January 9, 2026, [https://hypershift.pages.dev/reference/konnectivity/](https://hypershift.pages.dev/reference/konnectivity/)
    
35.  35.Network addresses (proto) — envoy 1.37.0-dev-656c08 documentation, accessed January 9, 2026, [https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/core/v3/address.proto](https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/core/v3/address.proto)
    
36.  36.Listener configuration (proto) — envoy 1.37.0-dev-656c08 documentation, accessed January 9, 2026, [https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/listener/v3/listener.proto](https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/listener/v3/listener.proto)
    
37.  37.Resolving Common Errors in Envoy Proxy: A Troubleshooting Guide - HashiCorp Support, accessed January 9, 2026, [https://support.hashicorp.com/hc/en-us/articles/5295078989075-Resolving-Common-Errors-in-Envoy-Proxy-A-Troubleshooting-Guide](https://support.hashicorp.com/hc/en-us/articles/5295078989075-Resolving-Common-Errors-in-Envoy-Proxy-A-Troubleshooting-Guide)
    
38.  38.Explanation Of Envoy Config Part-1 | by Think, Write, Repeat | Medium, accessed January 9, 2026, [https://medium.com/@anusha.rpav/explanation-of-envoy-config-part-1-86e543d7d905](https://medium.com/@anusha.rpav/explanation-of-envoy-config-part-1-86e543d7d905)
    
39.  39.Service Discovery with Envoy - Stack Overflow, accessed January 9, 2026, [https://stackoverflow.com/questions/68495618/service-discovery-with-envoy](https://stackoverflow.com/questions/68495618/service-discovery-with-envoy)
    
40.  40.Cluster configuration (proto) — envoy 1.37.0-dev-a67180 documentation, accessed January 9, 2026, [https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/cluster/v3/cluster.proto](https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/cluster/v3/cluster.proto)
    
41.  41.How do I force Kubernetes CoreDNS to reload its Config Map after a change?, accessed January 9, 2026, [https://stackoverflow.com/questions/53498438/how-do-i-force-kubernetes-coredns-to-reload-its-config-map-after-a-change](https://stackoverflow.com/questions/53498438/how-do-i-force-kubernetes-coredns-to-reload-its-config-map-after-a-change)
    
42.  42.Envoy hot restart. This is the 2nd in my series of posts… | by Matt Klein, accessed January 9, 2026, [https://blog.envoyproxy.io/envoy-hot-restart-1d16b14555b5](https://blog.envoyproxy.io/envoy-hot-restart-1d16b14555b5)
    
43.  43.Chapter 4. Secondary networks | Multiple networks | OpenShift Container Platform | 4.19 | Red Hat Documentation, accessed January 9, 2026, [https://docs.redhat.com/en/documentation/openshift\_container\_platform/4.19/html/multiple\_networks/secondary-networks](https://docs.redhat.com/en/documentation/openshift_container_platform/4.19/html/multiple_networks/secondary-networks)
    
44.  44.Configuration: Static — envoy 1.37.0-dev-656c08 documentation, accessed January 9, 2026, [https://www.envoyproxy.io/docs/envoy/latest/start/quick-start/configuration-static](https://www.envoyproxy.io/docs/envoy/latest/start/quick-start/configuration-static)
    
45.  45.Listeners — envoy 1.37.0-dev-d7b83e documentation, accessed January 9, 2026, [https://www.envoyproxy.io/docs/envoy/latest/intro/arch\_overview/listeners/listeners](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/listeners/listeners)