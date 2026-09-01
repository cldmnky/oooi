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

package network

import (
	"fmt"
	"net"

	"github.com/cldmnky/oooi/api/v1alpha1"
)

// Normalize converts a v1alpha1.NetworkConfig into the normalized Network
// model. It is a pure function: it performs no Kubernetes client I/O.
//
// The legacy form (networkAttachmentDefinition) is preserved for compatibility:
// it normalizes to a Secondary, Localnet, Layer2-exposed NetworkAttachmentDefinition
// attachment whose IPAM is DHCP when the oooi DHCP server is enabled and
// External otherwise.
//
// The structured form (attachment plus topology, role, exposure and ipam) is
// only accepted when no legacy field is set; mixed input is rejected. Phase 1
// support is IPv4-only and rejects Layer3 and every unsupported matrix
// combination (see validateNetwork).
//
// infraNamespace is the namespace of the owning Infra resource and is used as
// the default namespace for NetworkAttachmentDefinition attachments.
func Normalize(cfg v1alpha1.NetworkConfig, infraNamespace string, dhcpEnabled bool) (Network, error) {
	legacy := cfg.NetworkAttachmentDefinition != "" || cfg.NetworkAttachmentNamespace != ""
	structured := cfg.Attachment != nil || cfg.Topology != "" || cfg.Role != "" ||
		cfg.Exposure != "" || cfg.IPAM != "" || cfg.RouteAdvertisementsRef != nil

	if legacy && structured {
		return Network{}, fmt.Errorf("cannot mix legacy networkAttachmentDefinition/networkAttachmentNamespace with structured attachment/topology/role/exposure/ipam/routeAdvertisementsRef")
	}
	if !legacy && !structured {
		return Network{}, fmt.Errorf("must specify either attachment or networkAttachmentDefinition")
	}

	out := Network{
		CIDR:    cfg.CIDR,
		Gateway: cfg.Gateway,
	}
	if err := validateCIDR(out.CIDR); err != nil {
		return Network{}, err
	}

	if legacy {
		if err := normalizeLegacy(cfg, infraNamespace, dhcpEnabled, &out); err != nil {
			return Network{}, err
		}
	} else {
		if err := normalizeStructured(cfg, infraNamespace, &out); err != nil {
			return Network{}, err
		}
	}

	if err := validateNetwork(&out, dhcpEnabled); err != nil {
		return Network{}, err
	}

	out.DNSServers = append([]string(nil), cfg.DNSServers...)
	return out, nil
}

// normalizeLegacy converts the legacy NAD form. Structured fields are
// guaranteed absent by the caller.
func normalizeLegacy(cfg v1alpha1.NetworkConfig, infraNamespace string, dhcpEnabled bool, out *Network) error {
	if cfg.NetworkAttachmentDefinition == "" {
		return fmt.Errorf("networkAttachmentNamespace requires networkAttachmentDefinition")
	}
	out.Attachment = AttachmentReference{
		Kind:      KindNetworkAttachmentDefinition,
		Name:      cfg.NetworkAttachmentDefinition,
		Namespace: cfg.NetworkAttachmentNamespace,
	}
	if out.Attachment.Namespace == "" {
		out.Attachment.Namespace = infraNamespace
	}
	out.Topology = TopologyLocalnet
	out.Role = RoleSecondary
	out.Exposure = ExposureLayer2
	if dhcpEnabled {
		out.IPAM = IPAMModeDHCP
	} else {
		out.IPAM = IPAMModeExternal
	}
	return nil
}

// normalizeStructured converts the structured attachment form. The attachment
// pointer is guaranteed non-nil by the caller.
func normalizeStructured(cfg v1alpha1.NetworkConfig, infraNamespace string, out *Network) error {
	if cfg.Attachment == nil {
		return fmt.Errorf("attachment is required when using structured network fields")
	}

	switch cfg.Attachment.Kind {
	case v1alpha1.NetworkAttachmentKindNetworkAttachmentDefinition:
		out.Attachment.Kind = KindNetworkAttachmentDefinition
	case v1alpha1.NetworkAttachmentKindClusterUserDefinedNetwork:
		out.Attachment.Kind = KindClusterUserDefinedNetwork
	default:
		return fmt.Errorf("unsupported attachment kind %q (supported: NetworkAttachmentDefinition, ClusterUserDefinedNetwork)", cfg.Attachment.Kind)
	}
	if cfg.Attachment.Name == "" {
		return fmt.Errorf("attachment name is required")
	}
	out.Attachment.Name = cfg.Attachment.Name
	out.Attachment.Namespace = cfg.Attachment.Namespace

	switch out.Attachment.Kind {
	case KindNetworkAttachmentDefinition:
		if out.Attachment.Namespace == "" {
			out.Attachment.Namespace = infraNamespace
		}
	case KindClusterUserDefinedNetwork:
		// The CUDN object is cluster-scoped, but the namespace identifies the
		// generated NADs and endpoints, so it must be explicit.
		if out.Attachment.Namespace == "" {
			return fmt.Errorf("ClusterUserDefinedNetwork attachment namespace is required (namespace of the generated NADs and endpoints)")
		}
	}

	var err error
	if out.Topology, err = requireTopology(cfg.Topology); err != nil {
		return err
	}
	if out.Role, err = requireRole(cfg.Role); err != nil {
		return err
	}
	if out.Exposure, err = requireExposure(cfg.Exposure); err != nil {
		return err
	}
	if out.IPAM, err = requireIPAM(cfg.IPAM); err != nil {
		return err
	}

	if cfg.RouteAdvertisementsRef != nil {
		if cfg.RouteAdvertisementsRef.Name == "" {
			return fmt.Errorf("routeAdvertisementsRef name is required")
		}
		ref := *cfg.RouteAdvertisementsRef
		out.RouteAdvRef = &RouteAdvertisementsReference{Name: ref.Name}
	}
	return nil
}

// validateNetwork applies the Phase 1 supported matrix shared by both input
// forms. Unsupported combinations are rejected with actionable errors.
func validateNetwork(out *Network, dhcpEnabled bool) error {
	// Primary networks must be ClusterUserDefinedNetwork attachments.
	if out.Role == RolePrimary && out.Attachment.Kind == KindNetworkAttachmentDefinition {
		return fmt.Errorf("role Primary requires a ClusterUserDefinedNetwork attachment")
	}

	// Phase 1 is IPv4-only and does not support Layer3 networks.
	if out.Topology == TopologyLayer3 {
		return fmt.Errorf("topology Layer3 is not supported in Phase 1")
	}

	// ClusterUserDefinedNetwork matrix: primary must be Layer2+BGP+OVNKubernetes
	// (with route advertisements); secondary must be Localnet+Layer2.
	if out.Attachment.Kind == KindClusterUserDefinedNetwork {
		if out.Role == RolePrimary {
			if out.Topology != TopologyLayer2 {
				return fmt.Errorf("primary ClusterUserDefinedNetwork requires Layer2 topology")
			}
			if out.Exposure != ExposureBGP {
				return fmt.Errorf("primary ClusterUserDefinedNetwork requires BGP exposure")
			}
			if out.IPAM != IPAMModeOVNKubernetes {
				return fmt.Errorf("primary ClusterUserDefinedNetwork requires OVNKubernetes IPAM")
			}
			if out.RouteAdvRef == nil {
				return fmt.Errorf("routeAdvertisementsRef is required for ClusterUserDefinedNetwork BGP networks")
			}
		} else {
			if out.Topology != TopologyLocalnet {
				return fmt.Errorf("secondary ClusterUserDefinedNetwork requires Localnet topology")
			}
			if out.Exposure != ExposureLayer2 {
				return fmt.Errorf("secondary ClusterUserDefinedNetwork requires Layer2 exposure (BGP is only supported for primary networks)")
			}
		}
	}

	// Classic NADs cannot use OVN-Kubernetes-managed IPAM.
	if out.Attachment.Kind == KindNetworkAttachmentDefinition && out.IPAM == IPAMModeOVNKubernetes {
		return fmt.Errorf("ipam OVNKubernetes is not supported for NetworkAttachmentDefinition; use DHCP or External")
	}

	// CUDN+BGP always requires route advertisements.
	if out.Exposure == ExposureBGP && out.Attachment.Kind == KindClusterUserDefinedNetwork && out.RouteAdvRef == nil {
		return fmt.Errorf("routeAdvertisementsRef is required for ClusterUserDefinedNetwork BGP networks")
	}

	// Localnet networks cannot advertise routes.
	if out.RouteAdvRef != nil && out.Topology == TopologyLocalnet {
		return fmt.Errorf("routeAdvertisementsRef is not allowed for Localnet topology")
	}

	// The oooi DHCP server flag must agree with structured ipam (legacy derives
	// ipam from the flag, so it is consistent by construction).
	if (out.IPAM == IPAMModeDHCP) != dhcpEnabled {
		return fmt.Errorf("infraComponents.dhcp.enabled must match ipam: set dhcp.enabled=true only when ipam=DHCP")
	}

	// Secondary DHCP/External networks require a valid gateway inside the CIDR.
	if out.Role == RoleSecondary && (out.IPAM == IPAMModeDHCP || out.IPAM == IPAMModeExternal) {
		if out.Gateway == "" {
			return fmt.Errorf("gateway is required for Secondary networks with DHCP or External IPAM")
		}
		if err := validateGateway(out.CIDR, out.Gateway); err != nil {
			return err
		}
	}
	return nil
}

func requireTopology(v v1alpha1.NetworkTopology) (Topology, error) {
	if v == "" {
		return "", fmt.Errorf("topology is required when using structured attachment")
	}
	switch v {
	case v1alpha1.NetworkTopologyLocalnet:
		return TopologyLocalnet, nil
	case v1alpha1.NetworkTopologyLayer2:
		return TopologyLayer2, nil
	case v1alpha1.NetworkTopologyLayer3:
		return TopologyLayer3, nil
	default:
		return "", fmt.Errorf("unsupported topology %q (supported: Localnet, Layer2, Layer3)", v)
	}
}

func requireRole(v v1alpha1.NetworkRole) (Role, error) {
	if v == "" {
		return "", fmt.Errorf("role is required when using structured attachment")
	}
	switch v {
	case v1alpha1.NetworkRoleSecondary:
		return RoleSecondary, nil
	case v1alpha1.NetworkRolePrimary:
		return RolePrimary, nil
	default:
		return "", fmt.Errorf("unsupported role %q (supported: Secondary, Primary)", v)
	}
}

func requireExposure(v v1alpha1.NetworkExposure) (Exposure, error) {
	if v == "" {
		return "", fmt.Errorf("exposure is required when using structured attachment")
	}
	switch v {
	case v1alpha1.NetworkExposureLayer2:
		return ExposureLayer2, nil
	case v1alpha1.NetworkExposureBGP:
		return ExposureBGP, nil
	default:
		return "", fmt.Errorf("unsupported exposure %q (supported: Layer2, BGP)", v)
	}
}

func requireIPAM(v v1alpha1.NetworkIPAMMode) (IPAMMode, error) {
	if v == "" {
		return "", fmt.Errorf("ipam is required when using structured attachment")
	}
	switch v {
	case v1alpha1.NetworkIPAMModeDHCP:
		return IPAMModeDHCP, nil
	case v1alpha1.NetworkIPAMModeExternal:
		return IPAMModeExternal, nil
	case v1alpha1.NetworkIPAMModeOVNKubernetes:
		return IPAMModeOVNKubernetes, nil
	default:
		return "", fmt.Errorf("unsupported ipam %q (supported: DHCP, External, OVNKubernetes)", v)
	}
}

// validateCIDR parses the CIDR as an IPv4 network. Initial support is IPv4
// only; IPv6 and malformed values are rejected.
func validateCIDR(cidr string) error {
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil || ip.To4() == nil || ipNet.IP.To4() == nil {
		return fmt.Errorf("cidr %q is not a valid IPv4 CIDR", cidr)
	}
	return nil
}

// validateGateway checks the gateway syntax and that it falls within the CIDR.
func validateGateway(cidr, gateway string) error {
	gw := net.ParseIP(gateway)
	if gw == nil || gw.To4() == nil {
		return fmt.Errorf("gateway %q is not a valid IPv4 address", gateway)
	}
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("cidr %q is not a valid IPv4 CIDR", cidr)
	}
	if !ipNet.Contains(gw) {
		return fmt.Errorf("gateway %q is not within cidr %q", gateway, cidr)
	}
	return nil
}
