/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// InfraSpec defines the desired state of Infra.
type InfraSpec struct {
	// NetworkConfig defines the secondary network (VLAN) configuration
	// for the hosted cluster's isolated network.
	// +kubebuilder:validation:Required
	NetworkConfig NetworkConfig `json:"networkConfig"`

	// InfraComponents defines the configuration for infrastructure services
	// (DHCP, DNS, Proxy) that bridge the isolated VLAN to the control plane.
	// +optional
	InfraComponents InfraComponents `json:"infraComponents,omitempty"`

	// AppsIngress defines optional configuration for hosted cluster *.apps ingress handling.
	// +optional
	AppsIngress AppsIngressConfig `json:"appsIngress,omitempty"`
}

// NetworkConfig defines the secondary network parameters for the isolated VLAN.
type NetworkConfig struct {
	// CIDR is the IP address range for the secondary network in CIDR notation.
	// Example: "192.168.100.0/24"
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^(?:[0-9]{1,3}\.){3}[0-9]{1,3}/[0-9]{1,2}$`
	CIDR string `json:"cidr"`

	// Gateway is the default gateway IP address for the secondary network.
	// Example: "192.168.100.1"
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^(?:[0-9]{1,3}\.){3}[0-9]{1,3}$`
	Gateway string `json:"gateway"`

	// NetworkAttachmentDefinition is the name of the Multus NetworkAttachmentDefinition
	// that represents the secondary VLAN.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	NetworkAttachmentDefinition string `json:"networkAttachmentDefinition"`

	// NetworkAttachmentNamespace is the namespace where the NetworkAttachmentDefinition resides.
	// If not specified, the operator will look for the NAD first in the current namespace,
	// then in the default namespace.
	// +optional
	NetworkAttachmentNamespace string `json:"networkAttachmentNamespace,omitempty"`

	// DNSServers is an optional list of upstream DNS servers for external resolution.
	// If not specified, the infrastructure DNS will use the pod's default resolvers.
	// +optional
	DNSServers []string `json:"dnsServers,omitempty"`
}

// InfraComponents defines the configuration for infrastructure services.
type InfraComponents struct {
	// DHCP configuration for dynamic IP assignment to tenant VMs.
	// +optional
	DHCP DHCPConfig `json:"dhcp,omitempty"`

	// DNS configuration for split-horizon CoreDNS service.
	// +optional
	DNS DNSConfig `json:"dns,omitempty"`

	// Proxy configuration for Envoy L4 proxy gateway.
	// +optional
	Proxy ProxyConfig `json:"proxy,omitempty"`
}

// DHCPConfig defines the DHCP server configuration.
type DHCPConfig struct {
	// Enabled determines whether the DHCP server should be deployed.
	// +optional
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`

	// ServerIP is the static IP address assigned to the DHCP server pod
	// on the secondary network. Must be within the NetworkConfig CIDR.
	// +optional
	ServerIP string `json:"serverIP,omitempty"`

	// RangeStart is the beginning of the DHCP IP address pool.
	// +optional
	RangeStart string `json:"rangeStart,omitempty"`

	// RangeEnd is the end of the DHCP IP address pool.
	// +optional
	RangeEnd string `json:"rangeEnd,omitempty"`

	// LeaseTime is the DHCP lease duration (e.g., "1h", "24h").
	// +optional
	// +kubebuilder:default="1h"
	LeaseTime string `json:"leaseTime,omitempty"`

	// Image is the container image for the DHCP server.
	// +optional
	Image string `json:"image,omitempty"`
}

// DNSConfig defines the CoreDNS server configuration for split-horizon DNS.
type DNSConfig struct {
	// Enabled determines whether the DNS server should be deployed.
	// +optional
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`

	// ServerIP is the static IP address assigned to the CoreDNS pod
	// on the secondary network. Must be within the NetworkConfig CIDR.
	// +optional
	ServerIP string `json:"serverIP,omitempty"`

	// BaseDomain is the base domain for the hosted cluster (e.g., "example.com").
	// Used to construct FQDNs for API server and routes.
	// +optional
	BaseDomain string `json:"baseDomain,omitempty"`

	// ClusterName is the name of the hosted cluster.
	// Used to construct FQDNs (e.g., "api.<clusterName>.<baseDomain>").
	// +optional
	ClusterName string `json:"clusterName,omitempty"`

	// Image is the container image for CoreDNS.
	// +optional
	Image string `json:"image,omitempty"`
}

// ProxyConfig defines the Envoy proxy configuration for L4 gateway.
type ProxyConfig struct {
	// Enabled determines whether the Envoy proxy should be deployed.
	// +optional
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`

	// ServerIP is the static IP address assigned to the Envoy proxy pod
	// on the secondary network. Must be within the NetworkConfig CIDR.
	// This is used for external access (VM/multus network).
	// +optional
	ServerIP string `json:"serverIP,omitempty"`

	// InternalProxyService is the internal proxy service for pod network access.
	// Can be a ClusterIP service name (e.g., "envoy-internal.namespace.svc.cluster.local")
	// or a ClusterIP address. Used by DNS default view for management cluster pod access.
	// +optional
	InternalProxyService string `json:"internalProxyService,omitempty"`

	// ControlPlaneNamespace is the namespace where the hosted control plane
	// services are running (e.g., "clusters-<clustername>").
	// +optional
	ControlPlaneNamespace string `json:"controlPlaneNamespace,omitempty"`

	// APIServerService is the name of the Kubernetes API server service
	// in the control plane namespace.
	// +optional
	// +kubebuilder:default="kube-apiserver"
	APIServerService string `json:"apiServerService,omitempty"`

	// ProxyImage is the container image for Envoy proxy.
	// +optional
	// +kubebuilder:default="envoyproxy/envoy:v1.36.4"
	ProxyImage string `json:"proxyImage,omitempty"`

	// ManagerImage is the container image for the xDS control plane (oooi).
	// +optional
	// +kubebuilder:default="quay.io/cldmnky/oooi:latest"
	ManagerImage string `json:"managerImage,omitempty"`
}

// AppsIngressConfig defines optional configuration for hosted cluster *.apps domain ingress handling.
type AppsIngressConfig struct {
	// Enabled determines whether apps ingress automation should be deployed.
	// +optional
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`

	// BaseDomain is the base domain for the hosted cluster's apps ingress.
	// Example: "apps.example.com" where *.apps.<cluster>.apps.example.com will be routable.
	// +optional
	BaseDomain string `json:"baseDomain,omitempty"`

	// HostedClusterRef references the HostedCluster for which apps ingress is being configured.
	// +optional
	HostedClusterRef HostedClusterReference `json:"hostedClusterRef,omitempty"`

	// MetalLB contains configuration for the user-managed MetalLB installation.
	// +optional
	MetalLB AppsIngressMetalLB `json:"metallb,omitempty"`

	// Service defines the LoadBalancer service to be created in the hosted cluster.
	// +optional
	Service AppsIngressService `json:"service,omitempty"`

	// Ports defines the HTTP/HTTPS ports for the ingress.
	// +optional
	Ports AppsIngressPorts `json:"ports,omitempty"`
}

// HostedClusterReference is a reference to a HostedCluster resource.
type HostedClusterReference struct {
	// Name is the name of the HostedCluster.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace is the namespace of the HostedCluster.
	// +optional
	// +kubebuilder:default="clusters"
	Namespace string `json:"namespace,omitempty"`
}

// AppsIngressMetalLB defines the MetalLB address pool scope for external IPs.
type AppsIngressMetalLB struct {
	// AddressPoolName is the MetalLB address pool name to use for the LoadBalancer service.
	// Example: "lab-network"
	// +optional
	AddressPoolName string `json:"addressPoolName,omitempty"`

	// IPAddressPoolRange defines the valid range of IPs expected to be allocated by MetalLB.
	// Format: "start-end" or "start/prefix" notation.
	// This is for documentation/validation purposes only; not enforced by the operator.
	// Example: "10.202.64.221-10.202.64.240"
	// +optional
	IPAddressPoolRange string `json:"ipAddressPoolRange,omitempty"`

	// L2AdvertisementName is the MetalLB L2Advertisement resource name.
	// +optional
	L2AdvertisementName string `json:"l2AdvertisementName,omitempty"`
}

// AppsIngressService defines the LoadBalancer service in the hosted cluster.
type AppsIngressService struct {
	// Name is the name of the LoadBalancer service in the hosted cluster.
	// +optional
	// +kubebuilder:default="oooi-ingress"
	Name string `json:"name,omitempty"`

	// Namespace is the namespace where the service will be created.
	// Default: "clusters-<hostedcluster-name>"
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// AppsIngressPorts defines the HTTP and HTTPS ports for the ingress.
type AppsIngressPorts struct {
	// HTTP port number.
	// +optional
	// +kubebuilder:default=80
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	HTTP int32 `json:"http,omitempty"`

	// HTTPS port number.
	// +optional
	// +kubebuilder:default=443
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	HTTPS int32 `json:"https,omitempty"`
}

// InfraStatus defines the observed state of Infra.
type InfraStatus struct {
	// Conditions represents the latest available observations of the Infra's state.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// ComponentStatus tracks the status of individual infrastructure components.
	// +optional
	ComponentStatus ComponentStatus `json:"componentStatus,omitempty"`

	// AppsIngressStatus tracks the status of apps ingress configuration.
	// +optional
	AppsIngressStatus AppsIngressStatus `json:"appsIngressStatus,omitempty"`

	// ObservedGeneration reflects the generation of the most recently observed Infra.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// ComponentStatus tracks the readiness of infrastructure components.
type ComponentStatus struct {
	// DHCPReady indicates whether the DHCP server is ready.
	// +optional
	DHCPReady bool `json:"dhcpReady,omitempty"`

	// DNSReady indicates whether the CoreDNS server is ready.
	// +optional
	DNSReady bool `json:"dnsReady,omitempty"`

	// ProxyReady indicates whether the Envoy proxy is ready.
	// +optional
	ProxyReady bool `json:"proxyReady,omitempty"`
}

// AppsIngressStatus tracks the status of apps ingress configuration.
type AppsIngressStatus struct {
	// Phase represents the current phase of apps ingress provisioning.
	// Possible values: Pending, Ready, Degraded
	// +optional
	Phase string `json:"phase,omitempty"`

	// ExternalIP is the external IP assigned by MetalLB to the hosted cluster's LoadBalancer service.
	// +optional
	ExternalIP string `json:"externalIP,omitempty"`

	// LastSyncTime is the timestamp of the last successful reconciliation.
	// +optional
	LastSyncTime metav1.Time `json:"lastSyncTime,omitempty"`

	// Reason provides a short reason for the current phase.
	// +optional
	Reason string `json:"reason,omitempty"`

	// Message provides a human-readable message explaining the current status.
	// +optional
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=infra
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status",description="Ready status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Infra is the Schema for the infras API.
type Infra struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InfraSpec   `json:"spec,omitempty"`
	Status InfraStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// InfraList contains a list of Infra.
type InfraList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Infra `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Infra{}, &InfraList{})
}
