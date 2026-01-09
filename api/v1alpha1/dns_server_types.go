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

// DNSServerSpec defines the desired state of DNSServer
type DNSServerSpec struct {
	// NetworkConfig defines the network parameters for the DNS server
	NetworkConfig DNSNetworkConfig `json:"networkConfig"`

	// HostedClusterDomain is the base domain for the hosted control plane
	// Example: "my-cluster.example.com"
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	HostedClusterDomain string `json:"hostedClusterDomain"`

	// StaticEntries defines static DNS A records for control plane endpoints
	// +optional
	StaticEntries []DNSStaticEntry `json:"staticEntries,omitempty"`

	// UpstreamDNS defines upstream DNS servers for non-HCP domain resolution
	// +optional
	UpstreamDNS []string `json:"upstreamDNS,omitempty"`

	// Image is the container image for the DNS server
	// +optional
	// +kubebuilder:default="quay.io/cldmnky/oooi:latest"
	Image string `json:"image,omitempty"`

	// ReloadInterval is how often CoreDNS checks for Corefile changes
	// +optional
	// +kubebuilder:default="5s"
	// +kubebuilder:validation:Pattern=`^[0-9]+(s|m|h)$`
	ReloadInterval string `json:"reloadInterval,omitempty"`

	// CacheTTL is the DNS response cache time-to-live
	// +optional
	// +kubebuilder:default="30s"
	// +kubebuilder:validation:Pattern=`^[0-9]+(s|m|h)$`
	CacheTTL string `json:"cacheTTL,omitempty"`
}

// DNSNetworkConfig defines the network configuration for the DNS server
type DNSNetworkConfig struct {
	// ServerIP is the static IP address assigned to the DNS server on the secondary network
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^(?:[0-9]{1,3}\.){3}[0-9]{1,3}$`
	ServerIP string `json:"serverIP"`

	// ProxyIP is the IP address of the Envoy L4 proxy that DNS entries will point to
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^(?:[0-9]{1,3}\.){3}[0-9]{1,3}$`
	ProxyIP string `json:"proxyIP"`

	// SecondaryNetworkCIDR is the CIDR of the secondary network for view plugin matching
	// Queries from this CIDR will see HCP endpoints (split-horizon)
	// +optional
	// +kubebuilder:validation:Pattern=`^(?:[0-9]{1,3}\.){3}[0-9]{1,3}/[0-9]{1,2}$`
	SecondaryNetworkCIDR string `json:"secondaryNetworkCIDR,omitempty"`

	// NetworkAttachmentName is the name of the NetworkAttachmentDefinition to attach
	// +optional
	NetworkAttachmentName string `json:"networkAttachmentName,omitempty"`

	// NetworkAttachmentNamespace is the namespace of the NetworkAttachmentDefinition
	// +optional
	NetworkAttachmentNamespace string `json:"networkAttachmentNamespace,omitempty"`

	// DNSPort is the port the DNS server listens on
	// +optional
	// +kubebuilder:default=53
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	DNSPort int32 `json:"dnsPort,omitempty"`
}

// DNSStaticEntry defines a static DNS record
type DNSStaticEntry struct {
	// Hostname is the fully qualified domain name
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Hostname string `json:"hostname"`

	// IP is the IPv4 address this hostname resolves to
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^(?:[0-9]{1,3}\.){3}[0-9]{1,3}$`
	IP string `json:"ip"`
}

// DNSServerStatus defines the observed state of DNSServer
type DNSServerStatus struct {
	// Conditions represents the latest available observations of the DNSServer's state
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// ConfigMapName is the name of the ConfigMap containing the Corefile
	// +optional
	ConfigMapName string `json:"configMapName,omitempty"`

	// DeploymentName is the name of the Deployment running the DNS server
	// +optional
	DeploymentName string `json:"deploymentName,omitempty"`

	// ObservedGeneration reflects the generation of the most recently observed DNSServer
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=dns
// +kubebuilder:printcolumn:name="Domain",type=string,JSONPath=`.spec.hostedClusterDomain`
// +kubebuilder:printcolumn:name="ServerIP",type=string,JSONPath=`.spec.networkConfig.serverIP`
// +kubebuilder:printcolumn:name="ProxyIP",type=string,JSONPath=`.spec.networkConfig.proxyIP`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// DNSServer is the Schema for the dnsservers API
type DNSServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DNSServerSpec   `json:"spec,omitempty"`
	Status DNSServerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DNSServerList contains a list of DNSServer
type DNSServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DNSServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DNSServer{}, &DNSServerList{})
}
