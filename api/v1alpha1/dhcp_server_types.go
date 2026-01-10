/*
Copyright 2024 Magnus Bengtsson.

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

// DHCPServerSpec defines the desired state of DHCPServer
type DHCPServerSpec struct {
	// NetworkConfig defines the network parameters for the DHCP server
	NetworkConfig DHCPNetworkConfig `json:"networkConfig"`

	// LeaseConfig defines the IP address lease configuration
	LeaseConfig DHCPLeaseConfig `json:"leaseConfig"`

	// Options defines additional DHCP options to serve
	// +optional
	Options []DHCPOption `json:"options,omitempty"`

	// Image is the container image for the DHCP server
	// +optional
	// +kubebuilder:default="ghcr.io/cldmnky/hyperdhcp:latest"
	Image string `json:"image,omitempty"`
}

// DHCPNetworkConfig defines the network configuration for the DHCP server
type DHCPNetworkConfig struct {
	// CIDR is the IP address range that this DHCP server manages
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^(?:[0-9]{1,3}\.){3}[0-9]{1,3}/[0-9]{1,2}$`
	CIDR string `json:"cidr"`

	// Gateway is the default gateway IP address
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^(?:[0-9]{1,3}\.){3}[0-9]{1,3}$`
	Gateway string `json:"gateway"`

	// ServerIP is the static IP address assigned to the DHCP server
	// Can be specified with or without CIDR notation (e.g., "192.168.1.2" or "192.168.1.2/24")
	// If CIDR is omitted, /24 will be assumed for static IPAM
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^(?:[0-9]{1,3}\.){3}[0-9]{1,3}(?:/[0-9]{1,2})?$`
	ServerIP string `json:"serverIP"`

	// DNSServers is a list of DNS servers to advertise to clients
	// +optional
	DNSServers []string `json:"dnsServers,omitempty"`

	// NetworkAttachmentName is the name of the NetworkAttachmentDefinition to attach
	// +optional
	NetworkAttachmentName string `json:"networkAttachmentName,omitempty"`

	// NetworkAttachmentNamespace is the namespace of the NetworkAttachmentDefinition
	// +optional
	NetworkAttachmentNamespace string `json:"networkAttachmentNamespace,omitempty"`
}

// DHCPLeaseConfig defines the IP lease configuration
type DHCPLeaseConfig struct {
	// RangeStart is the beginning of the DHCP IP address pool
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^(?:[0-9]{1,3}\.){3}[0-9]{1,3}$`
	RangeStart string `json:"rangeStart"`

	// RangeEnd is the end of the DHCP IP address pool
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^(?:[0-9]{1,3}\.){3}[0-9]{1,3}$`
	RangeEnd string `json:"rangeEnd"`

	// LeaseTime is the DHCP lease duration (e.g., "1h", "24h")
	// +optional
	// +kubebuilder:default="1h"
	LeaseTime string `json:"leaseTime,omitempty"`
}

// DHCPOption defines a DHCP option to serve to clients
type DHCPOption struct {
	// Code is the DHCP option code (1-254)
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=254
	Code int `json:"code"`

	// Value is the value for this DHCP option
	// +kubebuilder:validation:Required
	Value string `json:"value"`
}

// DHCPServerStatus defines the observed state of DHCPServer
type DHCPServerStatus struct {
	// Conditions represents the latest available observations of the DHCPServer's state
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// ActiveLeases is the number of currently active DHCP leases
	// +optional
	ActiveLeases int32 `json:"activeLeases,omitempty"`

	// TotalLeases is the total number of available IP addresses in the pool
	// +optional
	TotalLeases int32 `json:"totalLeases,omitempty"`

	// ObservedGeneration reflects the generation of the most recently observed DHCPServer
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=dhcpserver
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status",description="Ready status"
// +kubebuilder:printcolumn:name="Active Leases",type="integer",JSONPath=".status.activeLeases",description="Active DHCP leases"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// DHCPServer is the Schema for the dhcpservers API
type DHCPServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DHCPServerSpec   `json:"spec,omitempty"`
	Status DHCPServerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DHCPServerList contains a list of DHCPServer
type DHCPServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DHCPServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DHCPServer{}, &DHCPServerList{})
}
