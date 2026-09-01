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

// NetworkAttachmentKind identifies the kind of the network attachment resource
// referenced by NetworkConfig.Attachment.
type NetworkAttachmentKind string

const (
	// NetworkAttachmentKindNetworkAttachmentDefinition references a namespaced
	// Multus NetworkAttachmentDefinition.
	NetworkAttachmentKindNetworkAttachmentDefinition NetworkAttachmentKind = "NetworkAttachmentDefinition"

	// NetworkAttachmentKindClusterUserDefinedNetwork references a cluster-scoped
	// OVN-Kubernetes ClusterUserDefinedNetwork. The CUDN object itself is
	// cluster-scoped, but attachment.namespace identifies the namespace of the
	// NADs and endpoints generated for it, so it must be set.
	NetworkAttachmentKindClusterUserDefinedNetwork NetworkAttachmentKind = "ClusterUserDefinedNetwork"
)

// NetworkTopology defines the layer-2/layer-3 behavior of a network.
type NetworkTopology string

const (
	// NetworkTopologyLocalnet is an isolated local network that is not
	// connected to the OVN-Kubernetes overlay.
	NetworkTopologyLocalnet NetworkTopology = "Localnet"

	// NetworkTopologyLayer2 is a bridged layer-2 network.
	NetworkTopologyLayer2 NetworkTopology = "Layer2"

	// NetworkTopologyLayer3 is a routed layer-3 network.
	NetworkTopologyLayer3 NetworkTopology = "Layer3"
)

// NetworkRole defines whether a network is the primary network of the cluster
// or an additional secondary network.
type NetworkRole string

const (
	// NetworkRoleSecondary marks a secondary (additional) network.
	NetworkRoleSecondary NetworkRole = "Secondary"

	// NetworkRolePrimary marks the cluster primary network.
	NetworkRolePrimary NetworkRole = "Primary"
)

// NetworkExposure defines how a network is exposed to the hosting cluster.
type NetworkExposure string

const (
	// NetworkExposureLayer2 exposes the network at layer 2.
	NetworkExposureLayer2 NetworkExposure = "Layer2"

	// NetworkExposureBGP exposes the network as a routed BGP network.
	NetworkExposureBGP NetworkExposure = "BGP"
)

// NetworkIPAMMode defines the IP address management mode of a network.
type NetworkIPAMMode string

const (
	// NetworkIPAMModeDHCP uses the oooi DHCP server for address assignment.
	NetworkIPAMModeDHCP NetworkIPAMMode = "DHCP"

	// NetworkIPAMModeExternal uses an externally managed IPAM.
	NetworkIPAMModeExternal NetworkIPAMMode = "External"

	// NetworkIPAMModeOVNKubernetes lets OVN-Kubernetes manage addresses.
	NetworkIPAMModeOVNKubernetes NetworkIPAMMode = "OVNKubernetes"
)

// NetworkAttachmentReference references the network attachment resource that
// represents a network.
type NetworkAttachmentReference struct {
	// Kind identifies the kind of the attachment resource. Supported values:
	// NetworkAttachmentDefinition, ClusterUserDefinedNetwork.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=NetworkAttachmentDefinition;ClusterUserDefinedNetwork
	Kind NetworkAttachmentKind `json:"kind"`

	// Name is the name of the attachment resource.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Namespace is the namespace of the attachment resource. For
	// NetworkAttachmentDefinition it defaults to the Infra namespace when
	// empty. For the cluster-scoped ClusterUserDefinedNetwork kind it is
	// required: it identifies the namespace of the NADs and endpoints
	// generated for the CUDN.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// ClusterResourceReference references a cluster-scoped resource by name.
type ClusterResourceReference struct {
	// Name is the name of the cluster-scoped resource.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}
