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
