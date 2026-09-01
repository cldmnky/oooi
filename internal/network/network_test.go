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
	"reflect"
	"strings"
	"testing"

	"github.com/cldmnky/oooi/api/v1alpha1"
)

const (
	testCIDR      = "192.168.100.0/24"
	testGateway   = "192.168.100.1"
	testInfraNS   = "infra-ns"
	testLegacyNAD = "tenant-vlan-100"
	testCUDNNS    = "cluster-net"
)

func nadRef(name, namespace string) *v1alpha1.NetworkAttachmentReference {
	return &v1alpha1.NetworkAttachmentReference{
		Kind:      v1alpha1.NetworkAttachmentKindNetworkAttachmentDefinition,
		Name:      name,
		Namespace: namespace,
	}
}

func cudnRef(name, namespace string) *v1alpha1.NetworkAttachmentReference {
	return &v1alpha1.NetworkAttachmentReference{
		Kind:      v1alpha1.NetworkAttachmentKindClusterUserDefinedNetwork,
		Name:      name,
		Namespace: namespace,
	}
}

func advRef(name string) *v1alpha1.ClusterResourceReference {
	return &v1alpha1.ClusterResourceReference{Name: name}
}

func legacyConfig(namespace string) v1alpha1.NetworkConfig {
	return v1alpha1.NetworkConfig{
		CIDR:                        testCIDR,
		Gateway:                     testGateway,
		NetworkAttachmentDefinition: testLegacyNAD,
		NetworkAttachmentNamespace:  namespace,
	}
}

func TestNormalizeNetworkConfig(t *testing.T) {
	tests := []struct {
		name           string
		cfg            v1alpha1.NetworkConfig
		infraNamespace string
		dhcpEnabled    bool
		want           Network
		wantErr        string // expected error substring; empty means success
	}{
		{
			name:           "legacy NAD with empty namespace defaults to infra namespace",
			cfg:            legacyConfig(""),
			infraNamespace: testInfraNS,
			dhcpEnabled:    true,
			want: Network{
				CIDR:    testCIDR,
				Gateway: testGateway,
				Attachment: AttachmentReference{
					Kind:      KindNetworkAttachmentDefinition,
					Name:      testLegacyNAD,
					Namespace: testInfraNS,
				},
				Topology: TopologyLocalnet,
				Role:     RoleSecondary,
				Exposure: ExposureLayer2,
				IPAM:     IPAMModeDHCP,
			},
		},
		{
			name:           "legacy NAD with explicit namespace preserved",
			cfg:            legacyConfig("vlan-ns"),
			infraNamespace: testInfraNS,
			dhcpEnabled:    true,
			want: Network{
				CIDR:    testCIDR,
				Gateway: testGateway,
				Attachment: AttachmentReference{
					Kind:      KindNetworkAttachmentDefinition,
					Name:      testLegacyNAD,
					Namespace: "vlan-ns",
				},
				Topology: TopologyLocalnet,
				Role:     RoleSecondary,
				Exposure: ExposureLayer2,
				IPAM:     IPAMModeDHCP,
			},
		},
		{
			name:           "legacy NAD uses External IPAM when DHCP disabled",
			cfg:            legacyConfig(""),
			infraNamespace: testInfraNS,
			dhcpEnabled:    false,
			want: Network{
				CIDR:    testCIDR,
				Gateway: testGateway,
				Attachment: AttachmentReference{
					Kind:      KindNetworkAttachmentDefinition,
					Name:      testLegacyNAD,
					Namespace: testInfraNS,
				},
				Topology: TopologyLocalnet,
				Role:     RoleSecondary,
				Exposure: ExposureLayer2,
				IPAM:     IPAMModeExternal,
			},
		},
		{
			name: "legacy NAD copies DNS servers",
			cfg: func() v1alpha1.NetworkConfig {
				c := legacyConfig("")
				c.DNSServers = []string{"1.1.1.1", "8.8.8.8"}
				return c
			}(),
			infraNamespace: testInfraNS,
			dhcpEnabled:    false,
			want: Network{
				CIDR:    testCIDR,
				Gateway: testGateway,
				Attachment: AttachmentReference{
					Kind:      KindNetworkAttachmentDefinition,
					Name:      testLegacyNAD,
					Namespace: testInfraNS,
				},
				Topology:   TopologyLocalnet,
				Role:       RoleSecondary,
				Exposure:   ExposureLayer2,
				IPAM:       IPAMModeExternal,
				DNSServers: []string{"1.1.1.1", "8.8.8.8"},
			},
		},
		{
			name: "structured NAD with explicit namespace",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Gateway:    testGateway,
				Attachment: nadRef("tenant-vlan-200", "vlan-ns"),
				Topology:   v1alpha1.NetworkTopologyLocalnet,
				Role:       v1alpha1.NetworkRoleSecondary,
				Exposure:   v1alpha1.NetworkExposureLayer2,
				IPAM:       v1alpha1.NetworkIPAMModeExternal,
			},
			infraNamespace: testInfraNS,
			dhcpEnabled:    false,
			want: Network{
				CIDR:    testCIDR,
				Gateway: testGateway,
				Attachment: AttachmentReference{
					Kind:      KindNetworkAttachmentDefinition,
					Name:      "tenant-vlan-200",
					Namespace: "vlan-ns",
				},
				Topology: TopologyLocalnet,
				Role:     RoleSecondary,
				Exposure: ExposureLayer2,
				IPAM:     IPAMModeExternal,
			},
		},
		{
			name: "structured NAD with empty namespace defaults to infra namespace",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Gateway:    testGateway,
				Attachment: nadRef("tenant-vlan-200", ""),
				Topology:   v1alpha1.NetworkTopologyLocalnet,
				Role:       v1alpha1.NetworkRoleSecondary,
				Exposure:   v1alpha1.NetworkExposureLayer2,
				IPAM:       v1alpha1.NetworkIPAMModeDHCP,
			},
			infraNamespace: testInfraNS,
			dhcpEnabled:    true,
			want: Network{
				CIDR:    testCIDR,
				Gateway: testGateway,
				Attachment: AttachmentReference{
					Kind:      KindNetworkAttachmentDefinition,
					Name:      "tenant-vlan-200",
					Namespace: testInfraNS,
				},
				Topology: TopologyLocalnet,
				Role:     RoleSecondary,
				Exposure: ExposureLayer2,
				IPAM:     IPAMModeDHCP,
			},
		},
		{
			name: "secondary Localnet CUDN with External IPAM",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Gateway:    testGateway,
				Attachment: cudnRef("sec-cudn", testCUDNNS),
				Topology:   v1alpha1.NetworkTopologyLocalnet,
				Role:       v1alpha1.NetworkRoleSecondary,
				Exposure:   v1alpha1.NetworkExposureLayer2,
				IPAM:       v1alpha1.NetworkIPAMModeExternal,
			},
			infraNamespace: testInfraNS,
			dhcpEnabled:    false,
			want: Network{
				CIDR:    testCIDR,
				Gateway: testGateway,
				Attachment: AttachmentReference{
					Kind:      KindClusterUserDefinedNetwork,
					Name:      "sec-cudn",
					Namespace: testCUDNNS,
				},
				Topology: TopologyLocalnet,
				Role:     RoleSecondary,
				Exposure: ExposureLayer2,
				IPAM:     IPAMModeExternal,
			},
		},
		{
			name: "secondary Localnet CUDN with DHCP when enabled",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Gateway:    testGateway,
				Attachment: cudnRef("sec-cudn", testCUDNNS),
				Topology:   v1alpha1.NetworkTopologyLocalnet,
				Role:       v1alpha1.NetworkRoleSecondary,
				Exposure:   v1alpha1.NetworkExposureLayer2,
				IPAM:       v1alpha1.NetworkIPAMModeDHCP,
			},
			infraNamespace: testInfraNS,
			dhcpEnabled:    true,
			want: Network{
				CIDR:    testCIDR,
				Gateway: testGateway,
				Attachment: AttachmentReference{
					Kind:      KindClusterUserDefinedNetwork,
					Name:      "sec-cudn",
					Namespace: testCUDNNS,
				},
				Topology: TopologyLocalnet,
				Role:     RoleSecondary,
				Exposure: ExposureLayer2,
				IPAM:     IPAMModeDHCP,
			},
		},
		{
			name: "routed BGP secondary NAD with routeAdvertisementsRef",
			cfg: v1alpha1.NetworkConfig{
				CIDR:                   testCIDR,
				Gateway:                testGateway,
				Attachment:             nadRef("routed-nad", testInfraNS),
				Topology:               v1alpha1.NetworkTopologyLayer2,
				Role:                   v1alpha1.NetworkRoleSecondary,
				Exposure:               v1alpha1.NetworkExposureBGP,
				IPAM:                   v1alpha1.NetworkIPAMModeExternal,
				RouteAdvertisementsRef: advRef("routed-adv"),
			},
			infraNamespace: testInfraNS,
			dhcpEnabled:    false,
			want: Network{
				CIDR:    testCIDR,
				Gateway: testGateway,
				Attachment: AttachmentReference{
					Kind:      KindNetworkAttachmentDefinition,
					Name:      "routed-nad",
					Namespace: testInfraNS,
				},
				Topology:    TopologyLayer2,
				Role:        RoleSecondary,
				Exposure:    ExposureBGP,
				IPAM:        IPAMModeExternal,
				RouteAdvRef: &RouteAdvertisementsReference{Name: "routed-adv"},
			},
		},
		{
			name: "routed BGP secondary NAD without routeAdvertisementsRef",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Gateway:    testGateway,
				Attachment: nadRef("routed-nad", testInfraNS),
				Topology:   v1alpha1.NetworkTopologyLayer2,
				Role:       v1alpha1.NetworkRoleSecondary,
				Exposure:   v1alpha1.NetworkExposureBGP,
				IPAM:       v1alpha1.NetworkIPAMModeExternal,
			},
			infraNamespace: testInfraNS,
			dhcpEnabled:    false,
			want: Network{
				CIDR:    testCIDR,
				Gateway: testGateway,
				Attachment: AttachmentReference{
					Kind:      KindNetworkAttachmentDefinition,
					Name:      "routed-nad",
					Namespace: testInfraNS,
				},
				Topology: TopologyLayer2,
				Role:     RoleSecondary,
				Exposure: ExposureBGP,
				IPAM:     IPAMModeExternal,
			},
		},
		{
			name: "primary Layer2 BGP CUDN without gateway",
			cfg: v1alpha1.NetworkConfig{
				CIDR:                   "10.132.0.0/24",
				Attachment:             cudnRef("primary-cudn", testCUDNNS),
				Topology:               v1alpha1.NetworkTopologyLayer2,
				Role:                   v1alpha1.NetworkRolePrimary,
				Exposure:               v1alpha1.NetworkExposureBGP,
				IPAM:                   v1alpha1.NetworkIPAMModeOVNKubernetes,
				RouteAdvertisementsRef: advRef("primary-adv"),
			},
			infraNamespace: testInfraNS,
			dhcpEnabled:    false,
			want: Network{
				CIDR: "10.132.0.0/24",
				Attachment: AttachmentReference{
					Kind:      KindClusterUserDefinedNetwork,
					Name:      "primary-cudn",
					Namespace: testCUDNNS,
				},
				Topology:    TopologyLayer2,
				Role:        RolePrimary,
				Exposure:    ExposureBGP,
				IPAM:        IPAMModeOVNKubernetes,
				RouteAdvRef: &RouteAdvertisementsReference{Name: "primary-adv"},
			},
		},
		{
			name: "primary Layer2 BGP CUDN with optional gateway",
			cfg: v1alpha1.NetworkConfig{
				CIDR:                   "10.132.0.0/24",
				Gateway:                "10.132.0.1",
				Attachment:             cudnRef("primary-cudn", testCUDNNS),
				Topology:               v1alpha1.NetworkTopologyLayer2,
				Role:                   v1alpha1.NetworkRolePrimary,
				Exposure:               v1alpha1.NetworkExposureBGP,
				IPAM:                   v1alpha1.NetworkIPAMModeOVNKubernetes,
				RouteAdvertisementsRef: advRef("primary-adv"),
			},
			infraNamespace: testInfraNS,
			dhcpEnabled:    false,
			want: Network{
				CIDR:    "10.132.0.0/24",
				Gateway: "10.132.0.1",
				Attachment: AttachmentReference{
					Kind:      KindClusterUserDefinedNetwork,
					Name:      "primary-cudn",
					Namespace: testCUDNNS,
				},
				Topology:    TopologyLayer2,
				Role:        RolePrimary,
				Exposure:    ExposureBGP,
				IPAM:        IPAMModeOVNKubernetes,
				RouteAdvRef: &RouteAdvertisementsReference{Name: "primary-adv"},
			},
		},
		{
			name: "mixed legacy and structured attachment rejected",
			cfg: func() v1alpha1.NetworkConfig {
				c := legacyConfig("")
				c.Attachment = nadRef("tenant-vlan-200", "")
				return c
			}(),
			infraNamespace: testInfraNS,
			wantErr:        "cannot mix legacy",
		},
		{
			name: "legacy NAD with routeAdvertisementsRef rejected as mixed",
			cfg: func() v1alpha1.NetworkConfig {
				c := legacyConfig("")
				c.RouteAdvertisementsRef = advRef("routed-adv")
				return c
			}(),
			infraNamespace: testInfraNS,
			wantErr:        "cannot mix legacy",
		},
		{
			name: "legacy NAD with topology rejected as mixed",
			cfg: func() v1alpha1.NetworkConfig {
				c := legacyConfig("")
				c.Topology = v1alpha1.NetworkTopologyLayer2
				return c
			}(),
			infraNamespace: testInfraNS,
			wantErr:        "cannot mix legacy",
		},
		{
			name: "missing attachment form rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:    testCIDR,
				Gateway: testGateway,
			},
			infraNamespace: testInfraNS,
			wantErr:        "must specify either attachment or networkAttachmentDefinition",
		},
		{
			name: "networkAttachmentNamespace without definition rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:                       testCIDR,
				Gateway:                    testGateway,
				NetworkAttachmentNamespace: "vlan-ns",
			},
			infraNamespace: testInfraNS,
			wantErr:        "networkAttachmentNamespace requires networkAttachmentDefinition",
		},
		{
			name: "structured fields without attachment rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:     testCIDR,
				Gateway:  testGateway,
				Topology: v1alpha1.NetworkTopologyLayer2,
				Role:     v1alpha1.NetworkRoleSecondary,
				Exposure: v1alpha1.NetworkExposureLayer2,
				IPAM:     v1alpha1.NetworkIPAMModeExternal,
			},
			infraNamespace: testInfraNS,
			wantErr:        "attachment is required when using structured network fields",
		},
		{
			name: "attachment without name rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Gateway:    testGateway,
				Attachment: nadRef("", ""),
				Topology:   v1alpha1.NetworkTopologyLocalnet,
				Role:       v1alpha1.NetworkRoleSecondary,
				Exposure:   v1alpha1.NetworkExposureLayer2,
				IPAM:       v1alpha1.NetworkIPAMModeExternal,
			},
			infraNamespace: testInfraNS,
			wantErr:        "attachment name is required",
		},
		{
			name: "routeAdvertisementsRef without name rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:                   testCIDR,
				Gateway:                testGateway,
				Attachment:             nadRef("routed-nad", testInfraNS),
				Topology:               v1alpha1.NetworkTopologyLayer2,
				Role:                   v1alpha1.NetworkRoleSecondary,
				Exposure:               v1alpha1.NetworkExposureBGP,
				IPAM:                   v1alpha1.NetworkIPAMModeExternal,
				RouteAdvertisementsRef: advRef(""),
			},
			infraNamespace: testInfraNS,
			wantErr:        "routeAdvertisementsRef name is required",
		},
		{
			name: "CUDN without namespace rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Gateway:    testGateway,
				Attachment: cudnRef("sec-cudn", ""),
				Topology:   v1alpha1.NetworkTopologyLocalnet,
				Role:       v1alpha1.NetworkRoleSecondary,
				Exposure:   v1alpha1.NetworkExposureLayer2,
				IPAM:       v1alpha1.NetworkIPAMModeExternal,
			},
			infraNamespace: testInfraNS,
			wantErr:        "ClusterUserDefinedNetwork attachment namespace is required",
		},
		{
			name: "CUDN BGP secondary rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Gateway:    testGateway,
				Attachment: cudnRef("sec-cudn", testCUDNNS),
				Topology:   v1alpha1.NetworkTopologyLocalnet,
				Role:       v1alpha1.NetworkRoleSecondary,
				Exposure:   v1alpha1.NetworkExposureBGP,
				IPAM:       v1alpha1.NetworkIPAMModeExternal,
			},
			infraNamespace: testInfraNS,
			wantErr:        "secondary ClusterUserDefinedNetwork requires Layer2 exposure",
		},
		{
			name: "secondary CUDN with Layer2 topology rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Gateway:    testGateway,
				Attachment: cudnRef("sec-cudn", testCUDNNS),
				Topology:   v1alpha1.NetworkTopologyLayer2,
				Role:       v1alpha1.NetworkRoleSecondary,
				Exposure:   v1alpha1.NetworkExposureLayer2,
				IPAM:       v1alpha1.NetworkIPAMModeExternal,
			},
			infraNamespace: testInfraNS,
			wantErr:        "secondary ClusterUserDefinedNetwork requires Localnet topology",
		},
		{
			name: "primary CUDN with wrong topology rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:                   "10.132.0.0/24",
				Attachment:             cudnRef("primary-cudn", testCUDNNS),
				Topology:               v1alpha1.NetworkTopologyLocalnet,
				Role:                   v1alpha1.NetworkRolePrimary,
				Exposure:               v1alpha1.NetworkExposureBGP,
				IPAM:                   v1alpha1.NetworkIPAMModeOVNKubernetes,
				RouteAdvertisementsRef: advRef("primary-adv"),
			},
			infraNamespace: testInfraNS,
			wantErr:        "primary ClusterUserDefinedNetwork requires Layer2 topology",
		},
		{
			name: "primary CUDN with wrong exposure rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:                   "10.132.0.0/24",
				Attachment:             cudnRef("primary-cudn", testCUDNNS),
				Topology:               v1alpha1.NetworkTopologyLayer2,
				Role:                   v1alpha1.NetworkRolePrimary,
				Exposure:               v1alpha1.NetworkExposureLayer2,
				IPAM:                   v1alpha1.NetworkIPAMModeOVNKubernetes,
				RouteAdvertisementsRef: advRef("primary-adv"),
			},
			infraNamespace: testInfraNS,
			wantErr:        "primary ClusterUserDefinedNetwork requires BGP exposure",
		},
		{
			name: "primary CUDN with wrong IPAM rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:                   "10.132.0.0/24",
				Attachment:             cudnRef("primary-cudn", testCUDNNS),
				Topology:               v1alpha1.NetworkTopologyLayer2,
				Role:                   v1alpha1.NetworkRolePrimary,
				Exposure:               v1alpha1.NetworkExposureBGP,
				IPAM:                   v1alpha1.NetworkIPAMModeExternal,
				RouteAdvertisementsRef: advRef("primary-adv"),
			},
			infraNamespace: testInfraNS,
			wantErr:        "primary ClusterUserDefinedNetwork requires OVNKubernetes IPAM",
		},
		{
			name: "primary CUDN with DHCP rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:                   "10.132.0.0/24",
				Attachment:             cudnRef("primary-cudn", testCUDNNS),
				Topology:               v1alpha1.NetworkTopologyLayer2,
				Role:                   v1alpha1.NetworkRolePrimary,
				Exposure:               v1alpha1.NetworkExposureBGP,
				IPAM:                   v1alpha1.NetworkIPAMModeDHCP,
				RouteAdvertisementsRef: advRef("primary-adv"),
			},
			infraNamespace: testInfraNS,
			dhcpEnabled:    true,
			wantErr:        "primary ClusterUserDefinedNetwork requires OVNKubernetes IPAM",
		},
		{
			name: "primary raw NAD rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Gateway:    testGateway,
				Attachment: nadRef("tenant-vlan-200", testInfraNS),
				Topology:   v1alpha1.NetworkTopologyLayer2,
				Role:       v1alpha1.NetworkRolePrimary,
				Exposure:   v1alpha1.NetworkExposureLayer2,
				IPAM:       v1alpha1.NetworkIPAMModeExternal,
			},
			infraNamespace: testInfraNS,
			wantErr:        "role Primary requires a ClusterUserDefinedNetwork attachment",
		},
		{
			name: "NAD with OVNKubernetes IPAM rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:                   testCIDR,
				Gateway:                testGateway,
				Attachment:             nadRef("routed-nad", testInfraNS),
				Topology:               v1alpha1.NetworkTopologyLayer2,
				Role:                   v1alpha1.NetworkRoleSecondary,
				Exposure:               v1alpha1.NetworkExposureBGP,
				IPAM:                   v1alpha1.NetworkIPAMModeOVNKubernetes,
				RouteAdvertisementsRef: advRef("routed-adv"),
			},
			infraNamespace: testInfraNS,
			wantErr:        "ipam OVNKubernetes is not supported for NetworkAttachmentDefinition",
		},
		{
			name: "NAD with Layer3 topology rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Gateway:    testGateway,
				Attachment: nadRef("tenant-vlan-200", testInfraNS),
				Topology:   v1alpha1.NetworkTopologyLayer3,
				Role:       v1alpha1.NetworkRoleSecondary,
				Exposure:   v1alpha1.NetworkExposureLayer2,
				IPAM:       v1alpha1.NetworkIPAMModeExternal,
			},
			infraNamespace: testInfraNS,
			wantErr:        "topology Layer3 is not supported in Phase 1",
		},
		{
			name: "primary CUDN with Layer3 topology rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:                   "10.132.0.0/24",
				Attachment:             cudnRef("primary-cudn", testCUDNNS),
				Topology:               v1alpha1.NetworkTopologyLayer3,
				Role:                   v1alpha1.NetworkRolePrimary,
				Exposure:               v1alpha1.NetworkExposureBGP,
				IPAM:                   v1alpha1.NetworkIPAMModeOVNKubernetes,
				RouteAdvertisementsRef: advRef("primary-adv"),
			},
			infraNamespace: testInfraNS,
			wantErr:        "topology Layer3 is not supported in Phase 1",
		},
		{
			name: "Localnet CUDN with routeAdvertisementsRef rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:                   testCIDR,
				Gateway:                testGateway,
				Attachment:             cudnRef("sec-cudn", testCUDNNS),
				Topology:               v1alpha1.NetworkTopologyLocalnet,
				Role:                   v1alpha1.NetworkRoleSecondary,
				Exposure:               v1alpha1.NetworkExposureLayer2,
				IPAM:                   v1alpha1.NetworkIPAMModeExternal,
				RouteAdvertisementsRef: advRef("routed-adv"),
			},
			infraNamespace: testInfraNS,
			wantErr:        "routeAdvertisementsRef is not allowed for Localnet topology",
		},
		{
			name: "gateway required for DHCP secondary",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Attachment: nadRef("tenant-vlan-200", testInfraNS),
				Topology:   v1alpha1.NetworkTopologyLocalnet,
				Role:       v1alpha1.NetworkRoleSecondary,
				Exposure:   v1alpha1.NetworkExposureLayer2,
				IPAM:       v1alpha1.NetworkIPAMModeDHCP,
			},
			infraNamespace: testInfraNS,
			dhcpEnabled:    true,
			wantErr:        "gateway is required for Secondary networks with DHCP or External IPAM",
		},
		{
			name: "gateway required for External secondary",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Attachment: nadRef("tenant-vlan-200", testInfraNS),
				Topology:   v1alpha1.NetworkTopologyLocalnet,
				Role:       v1alpha1.NetworkRoleSecondary,
				Exposure:   v1alpha1.NetworkExposureLayer2,
				IPAM:       v1alpha1.NetworkIPAMModeExternal,
			},
			infraNamespace: testInfraNS,
			wantErr:        "gateway is required for Secondary networks with DHCP or External IPAM",
		},
		{
			name: "DHCP ipam with DHCP disabled rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Gateway:    testGateway,
				Attachment: nadRef("tenant-vlan-200", testInfraNS),
				Topology:   v1alpha1.NetworkTopologyLocalnet,
				Role:       v1alpha1.NetworkRoleSecondary,
				Exposure:   v1alpha1.NetworkExposureLayer2,
				IPAM:       v1alpha1.NetworkIPAMModeDHCP,
			},
			infraNamespace: testInfraNS,
			wantErr:        "must match",
		},
		{
			name: "External ipam with DHCP enabled rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Gateway:    testGateway,
				Attachment: nadRef("tenant-vlan-200", testInfraNS),
				Topology:   v1alpha1.NetworkTopologyLocalnet,
				Role:       v1alpha1.NetworkRoleSecondary,
				Exposure:   v1alpha1.NetworkExposureLayer2,
				IPAM:       v1alpha1.NetworkIPAMModeExternal,
			},
			infraNamespace: testInfraNS,
			dhcpEnabled:    true,
			wantErr:        "must match",
		},
		{
			name: "primary CUDN OVNKubernetes with DHCP enabled rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:                   "10.132.0.0/24",
				Attachment:             cudnRef("primary-cudn", testCUDNNS),
				Topology:               v1alpha1.NetworkTopologyLayer2,
				Role:                   v1alpha1.NetworkRolePrimary,
				Exposure:               v1alpha1.NetworkExposureBGP,
				IPAM:                   v1alpha1.NetworkIPAMModeOVNKubernetes,
				RouteAdvertisementsRef: advRef("primary-adv"),
			},
			infraNamespace: testInfraNS,
			dhcpEnabled:    true,
			wantErr:        "must match",
		},
		{
			name: "unknown attachment kind rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:    testCIDR,
				Gateway: testGateway,
				Attachment: &v1alpha1.NetworkAttachmentReference{
					Kind: v1alpha1.NetworkAttachmentKind("Bogus"),
					Name: "tenant-vlan-200",
				},
				Topology: v1alpha1.NetworkTopologyLocalnet,
				Role:     v1alpha1.NetworkRoleSecondary,
				Exposure: v1alpha1.NetworkExposureLayer2,
				IPAM:     v1alpha1.NetworkIPAMModeExternal,
			},
			infraNamespace: testInfraNS,
			wantErr:        "unsupported attachment kind",
		},
		{
			name: "unknown topology rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Gateway:    testGateway,
				Attachment: nadRef("tenant-vlan-200", testInfraNS),
				Topology:   v1alpha1.NetworkTopology("Bogus"),
				Role:       v1alpha1.NetworkRoleSecondary,
				Exposure:   v1alpha1.NetworkExposureLayer2,
				IPAM:       v1alpha1.NetworkIPAMModeExternal,
			},
			infraNamespace: testInfraNS,
			wantErr:        "unsupported topology",
		},
		{
			name: "unknown role rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Gateway:    testGateway,
				Attachment: nadRef("tenant-vlan-200", testInfraNS),
				Topology:   v1alpha1.NetworkTopologyLocalnet,
				Role:       v1alpha1.NetworkRole("Bogus"),
				Exposure:   v1alpha1.NetworkExposureLayer2,
				IPAM:       v1alpha1.NetworkIPAMModeExternal,
			},
			infraNamespace: testInfraNS,
			wantErr:        "unsupported role",
		},
		{
			name: "unknown exposure rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Gateway:    testGateway,
				Attachment: nadRef("tenant-vlan-200", testInfraNS),
				Topology:   v1alpha1.NetworkTopologyLocalnet,
				Role:       v1alpha1.NetworkRoleSecondary,
				Exposure:   v1alpha1.NetworkExposure("Bogus"),
				IPAM:       v1alpha1.NetworkIPAMModeExternal,
			},
			infraNamespace: testInfraNS,
			wantErr:        "unsupported exposure",
		},
		{
			name: "unknown ipam rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Gateway:    testGateway,
				Attachment: nadRef("tenant-vlan-200", testInfraNS),
				Topology:   v1alpha1.NetworkTopologyLocalnet,
				Role:       v1alpha1.NetworkRoleSecondary,
				Exposure:   v1alpha1.NetworkExposureLayer2,
				IPAM:       v1alpha1.NetworkIPAMMode("Bogus"),
			},
			infraNamespace: testInfraNS,
			wantErr:        "unsupported ipam",
		},
		{
			name: "missing topology rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Gateway:    testGateway,
				Attachment: nadRef("tenant-vlan-200", testInfraNS),
				Role:       v1alpha1.NetworkRoleSecondary,
				Exposure:   v1alpha1.NetworkExposureLayer2,
				IPAM:       v1alpha1.NetworkIPAMModeExternal,
			},
			infraNamespace: testInfraNS,
			wantErr:        "topology is required when using structured attachment",
		},
		{
			name: "missing role rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Gateway:    testGateway,
				Attachment: nadRef("tenant-vlan-200", testInfraNS),
				Topology:   v1alpha1.NetworkTopologyLocalnet,
				Exposure:   v1alpha1.NetworkExposureLayer2,
				IPAM:       v1alpha1.NetworkIPAMModeExternal,
			},
			infraNamespace: testInfraNS,
			wantErr:        "role is required when using structured attachment",
		},
		{
			name: "missing exposure rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Gateway:    testGateway,
				Attachment: nadRef("tenant-vlan-200", testInfraNS),
				Topology:   v1alpha1.NetworkTopologyLocalnet,
				Role:       v1alpha1.NetworkRoleSecondary,
				IPAM:       v1alpha1.NetworkIPAMModeExternal,
			},
			infraNamespace: testInfraNS,
			wantErr:        "exposure is required when using structured attachment",
		},
		{
			name: "missing ipam rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Gateway:    testGateway,
				Attachment: nadRef("tenant-vlan-200", testInfraNS),
				Topology:   v1alpha1.NetworkTopologyLocalnet,
				Role:       v1alpha1.NetworkRoleSecondary,
				Exposure:   v1alpha1.NetworkExposureLayer2,
			},
			infraNamespace: testInfraNS,
			wantErr:        "ipam is required when using structured attachment",
		},
		{
			name: "invalid CIDR rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       "not-a-cidr",
				Gateway:    testGateway,
				Attachment: nadRef("tenant-vlan-200", testInfraNS),
				Topology:   v1alpha1.NetworkTopologyLocalnet,
				Role:       v1alpha1.NetworkRoleSecondary,
				Exposure:   v1alpha1.NetworkExposureLayer2,
				IPAM:       v1alpha1.NetworkIPAMModeExternal,
			},
			infraNamespace: testInfraNS,
			wantErr:        "not a valid IPv4 CIDR",
		},
		{
			name: "IPv6 CIDR rejected in Phase 1",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       "fd00::/64",
				Gateway:    testGateway,
				Attachment: nadRef("tenant-vlan-200", testInfraNS),
				Topology:   v1alpha1.NetworkTopologyLocalnet,
				Role:       v1alpha1.NetworkRoleSecondary,
				Exposure:   v1alpha1.NetworkExposureLayer2,
				IPAM:       v1alpha1.NetworkIPAMModeExternal,
			},
			infraNamespace: testInfraNS,
			wantErr:        "not a valid IPv4 CIDR",
		},
		{
			name: "gateway outside CIDR rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Gateway:    "10.0.0.1",
				Attachment: nadRef("tenant-vlan-200", testInfraNS),
				Topology:   v1alpha1.NetworkTopologyLocalnet,
				Role:       v1alpha1.NetworkRoleSecondary,
				Exposure:   v1alpha1.NetworkExposureLayer2,
				IPAM:       v1alpha1.NetworkIPAMModeExternal,
			},
			infraNamespace: testInfraNS,
			wantErr:        "not within cidr",
		},
		{
			name: "gateway with invalid syntax rejected",
			cfg: v1alpha1.NetworkConfig{
				CIDR:       testCIDR,
				Gateway:    "not-an-ip",
				Attachment: nadRef("tenant-vlan-200", testInfraNS),
				Topology:   v1alpha1.NetworkTopologyLocalnet,
				Role:       v1alpha1.NetworkRoleSecondary,
				Exposure:   v1alpha1.NetworkExposureLayer2,
				IPAM:       v1alpha1.NetworkIPAMModeExternal,
			},
			infraNamespace: testInfraNS,
			wantErr:        "not a valid IPv4 address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Normalize(tt.cfg, tt.infraNamespace, tt.dhcpEnabled)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Normalize() error = nil, want error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Normalize() error = %q, want error containing %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Normalize() error = %v", err)
			}
			if !reflect.DeepEqual(tt.want, got) {
				t.Errorf("Normalize() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestNormalizeNetworkConfigInputIsolation verifies the normalized output does
// not alias mutable input slices or pointers.
func TestNormalizeNetworkConfigInputIsolation(t *testing.T) {
	cfg := v1alpha1.NetworkConfig{
		CIDR:                   testCIDR,
		Gateway:                testGateway,
		Attachment:             nadRef("routed-nad", testInfraNS),
		Topology:               v1alpha1.NetworkTopologyLayer2,
		Role:                   v1alpha1.NetworkRoleSecondary,
		Exposure:               v1alpha1.NetworkExposureBGP,
		IPAM:                   v1alpha1.NetworkIPAMModeExternal,
		RouteAdvertisementsRef: advRef("routed-adv"),
		DNSServers:             []string{"1.1.1.1"},
	}

	got, err := Normalize(cfg, testInfraNS, false)
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}

	// Mutating the input after normalization must not affect the output.
	cfg.DNSServers[0] = "9.9.9.9"
	cfg.RouteAdvertisementsRef.Name = "mutated"

	if got.DNSServers[0] != "1.1.1.1" {
		t.Errorf("DNSServers aliases input slice: got %v", got.DNSServers)
	}
	if got.RouteAdvRef == nil || got.RouteAdvRef.Name != "routed-adv" {
		t.Errorf("RouteAdvRef aliases input pointer: got %+v", got.RouteAdvRef)
	}
}
