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

// Package network provides the normalized model for the hosted cluster
// network configuration. Normalize converts both the legacy NAD-based and the
// structured attachment-based API forms into a single model without any
// Kubernetes client I/O.
package network

// Kind identifies the kind of the network attachment resource.
type Kind string

const (
	// KindNetworkAttachmentDefinition references a namespaced Multus
	// NetworkAttachmentDefinition.
	KindNetworkAttachmentDefinition Kind = "NetworkAttachmentDefinition"

	// KindClusterUserDefinedNetwork references a cluster-scoped OVN-Kubernetes
	// ClusterUserDefinedNetwork.
	KindClusterUserDefinedNetwork Kind = "ClusterUserDefinedNetwork"
)

// Topology defines the layer-2/layer-3 behavior of the network.
type Topology string

const (
	// TopologyLocalnet is an isolated local network that is not connected to
	// the OVN-Kubernetes overlay.
	TopologyLocalnet Topology = "Localnet"

	// TopologyLayer2 is a bridged layer-2 network.
	TopologyLayer2 Topology = "Layer2"

	// TopologyLayer3 is a routed layer-3 network.
	TopologyLayer3 Topology = "Layer3"
)

// Role defines whether the network is the primary network of the cluster or an
// additional secondary network.
type Role string

const (
	// RoleSecondary marks a secondary (additional) network.
	RoleSecondary Role = "Secondary"

	// RolePrimary marks the cluster primary network.
	RolePrimary Role = "Primary"
)

// Exposure defines how the network is exposed to the hosting cluster.
type Exposure string

const (
	// ExposureLayer2 exposes the network at layer 2.
	ExposureLayer2 Exposure = "Layer2"

	// ExposureBGP exposes the network as a routed BGP network.
	ExposureBGP Exposure = "BGP"
)

// IPAMMode defines the IP address management mode of the network.
type IPAMMode string

const (
	// IPAMModeDHCP uses the oooi DHCP server for address assignment.
	IPAMModeDHCP IPAMMode = "DHCP"

	// IPAMModeExternal uses an externally managed IPAM.
	IPAMModeExternal IPAMMode = "External"

	// IPAMModeOVNKubernetes lets OVN-Kubernetes manage addresses.
	IPAMModeOVNKubernetes IPAMMode = "OVNKubernetes"
)

// AttachmentReference identifies the network attachment resource that
// represents the network.
type AttachmentReference struct {
	// Kind is the kind of the attachment resource.
	Kind Kind
	// Name is the name of the attachment resource.
	Name string
	// Namespace is the namespace of the attachment resource. For
	// NetworkAttachmentDefinition it is the NAD namespace (defaulted from the
	// Infra namespace when unset). For ClusterUserDefinedNetwork it is
	// required and identifies the namespace of the NADs and endpoints
	// generated for the cluster-scoped CUDN.
	Namespace string
}

// RouteAdvertisementsReference identifies a cluster-scoped route
// advertisements resource. It is required for ClusterUserDefinedNetwork BGP
// exposure and optional intent metadata for classic NetworkAttachmentDefinition
// BGP exposure.
type RouteAdvertisementsReference struct {
	// Name is the name of the route advertisements resource.
	Name string
}

// Network is the normalized model of a hosted cluster network configuration.
// It contains no legacy duplicate fields: legacy and structured API input
// converge on this single representation.
type Network struct {
	// CIDR is the IP address range of the network in CIDR notation.
	CIDR string

	// Gateway is the default gateway of the network. Empty when the network
	// does not require one (e.g. primary networks with OVN-Kubernetes IPAM).
	Gateway string

	// Attachment identifies the underlying network attachment resource.
	Attachment AttachmentReference

	// Topology is the network topology.
	Topology Topology

	// Role is the network role.
	Role Role

	// Exposure is the network exposure.
	Exposure Exposure

	// IPAM is the IP address management mode.
	IPAM IPAMMode

	// RouteAdvRef references the route advertisements resource for routed
	// BGP-exposed networks. Nil when the network is not BGP-exposed.
	RouteAdvRef *RouteAdvertisementsReference

	// DNSServers is the list of upstream DNS servers for external resolution.
	DNSServers []string
}
