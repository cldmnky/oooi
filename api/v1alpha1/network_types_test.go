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
	"encoding/json"
	"os"
	"slices"
	"testing"

	"sigs.k8s.io/yaml"
)

// TestNetworkConfigStructuredJSON pins the exact JSON names of the structured
// Phase 1 fields added for issue #26 and verifies the persisted-compatible
// legacy fields keep their original JSON names.
func TestNetworkConfigStructuredJSON(t *testing.T) {
	tests := []struct {
		name string
		cfg  NetworkConfig
	}{
		{
			name: "structured fields serialize under issue-defined names",
			cfg: NetworkConfig{
				CIDR:    "192.168.100.0/24",
				Gateway: "192.168.100.1",
				Attachment: &NetworkAttachmentReference{
					Kind:      NetworkAttachmentKindClusterUserDefinedNetwork,
					Name:      "primary-cudn",
					Namespace: "cluster-net",
				},
				Topology:               NetworkTopologyLayer2,
				Role:                   NetworkRolePrimary,
				Exposure:               NetworkExposureBGP,
				IPAM:                   NetworkIPAMModeOVNKubernetes,
				RouteAdvertisementsRef: &ClusterResourceReference{Name: "primary-adv"},
			},
		},
		{
			name: "legacy fields keep their original JSON names",
			cfg: NetworkConfig{
				CIDR:                        "192.168.100.0/24",
				Gateway:                     "192.168.100.1",
				NetworkAttachmentDefinition: "tenant-vlan-100",
				NetworkAttachmentNamespace:  "vlan-ns",
				DNSServers:                  []string{"1.1.1.1"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.cfg)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}

			if m["cidr"] != tt.cfg.CIDR {
				t.Errorf("json cidr = %v, want %q", m["cidr"], tt.cfg.CIDR)
			}
			if tt.cfg.Gateway != "" && m["gateway"] != tt.cfg.Gateway {
				t.Errorf("json gateway = %v, want %q", m["gateway"], tt.cfg.Gateway)
			}

			if tt.cfg.Attachment != nil {
				att, ok := m["attachment"].(map[string]any)
				if !ok {
					t.Fatalf("json attachment missing: %s", data)
				}
				if att["kind"] != string(tt.cfg.Attachment.Kind) {
					t.Errorf("json attachment.kind = %v, want %q", att["kind"], tt.cfg.Attachment.Kind)
				}
				if att["name"] != tt.cfg.Attachment.Name {
					t.Errorf("json attachment.name = %v, want %q", att["name"], tt.cfg.Attachment.Name)
				}
				if tt.cfg.Attachment.Namespace != "" {
					if att["namespace"] != tt.cfg.Attachment.Namespace {
						t.Errorf("json attachment.namespace = %v, want %q", att["namespace"], tt.cfg.Attachment.Namespace)
					}
				} else if _, present := att["namespace"]; present {
					t.Errorf("json attachment.namespace should be omitted when empty: %s", data)
				}
				if m["topology"] != string(tt.cfg.Topology) {
					t.Errorf("json topology = %v, want %q", m["topology"], tt.cfg.Topology)
				}
				if m["role"] != string(tt.cfg.Role) {
					t.Errorf("json role = %v, want %q", m["role"], tt.cfg.Role)
				}
				if m["exposure"] != string(tt.cfg.Exposure) {
					t.Errorf("json exposure = %v, want %q", m["exposure"], tt.cfg.Exposure)
				}
				if m["ipam"] != string(tt.cfg.IPAM) {
					t.Errorf("json ipam = %v, want %q", m["ipam"], tt.cfg.IPAM)
				}
				ra, ok := m["routeAdvertisementsRef"].(map[string]any)
				if !ok {
					t.Fatalf("json routeAdvertisementsRef missing: %s", data)
				}
				if ra["name"] != tt.cfg.RouteAdvertisementsRef.Name {
					t.Errorf("json routeAdvertisementsRef.name = %v, want %q", ra["name"], tt.cfg.RouteAdvertisementsRef.Name)
				}
			} else {
				if m["networkAttachmentDefinition"] != tt.cfg.NetworkAttachmentDefinition {
					t.Errorf("json networkAttachmentDefinition = %v, want %q", m["networkAttachmentDefinition"], tt.cfg.NetworkAttachmentDefinition)
				}
				if m["networkAttachmentNamespace"] != tt.cfg.NetworkAttachmentNamespace {
					t.Errorf("json networkAttachmentNamespace = %v, want %q", m["networkAttachmentNamespace"], tt.cfg.NetworkAttachmentNamespace)
				}
				if _, present := m["attachment"]; present {
					t.Errorf("json attachment must be omitted for legacy configs: %s", data)
				}
				if _, present := m["topology"]; present {
					t.Errorf("json topology must be omitted for legacy configs: %s", data)
				}
				if _, present := m["routeAdvertisementsRef"]; present {
					t.Errorf("json routeAdvertisementsRef must be omitted for legacy configs: %s", data)
				}
			}
		})
	}
}

// TestNetworkConfigStructuredRoundTrip verifies unmarshalling restores the
// pointer-based attachment/reference fields and the value-based enum fields,
// and that empty enums round-trip as omitted fields.
func TestNetworkConfigStructuredRoundTrip(t *testing.T) {
	cfg := NetworkConfig{
		CIDR:    "192.168.100.0/24",
		Gateway: "192.168.100.1",
		Attachment: &NetworkAttachmentReference{
			Kind:      NetworkAttachmentKindNetworkAttachmentDefinition,
			Name:      "tenant-vlan-200",
			Namespace: "vlan-ns",
		},
		Topology:   NetworkTopologyLocalnet,
		Role:       NetworkRoleSecondary,
		Exposure:   NetworkExposureLayer2,
		IPAM:       NetworkIPAMModeExternal,
		DNSServers: []string{"1.1.1.1"},
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var got NetworkConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got.Attachment == nil {
		t.Fatal("Attachment is nil after round-trip")
	}
	if got.Attachment.Kind != NetworkAttachmentKindNetworkAttachmentDefinition || got.Attachment.Name != "tenant-vlan-200" || got.Attachment.Namespace != "vlan-ns" {
		t.Errorf("Attachment = %+v after round-trip", got.Attachment)
	}
	if got.Topology != NetworkTopologyLocalnet {
		t.Errorf("Topology = %q after round-trip, want %q", got.Topology, NetworkTopologyLocalnet)
	}
	if got.Role != NetworkRoleSecondary {
		t.Errorf("Role = %q after round-trip, want %q", got.Role, NetworkRoleSecondary)
	}
	if got.Exposure != NetworkExposureLayer2 {
		t.Errorf("Exposure = %q after round-trip, want %q", got.Exposure, NetworkExposureLayer2)
	}
	if got.IPAM != NetworkIPAMModeExternal {
		t.Errorf("IPAM = %q after round-trip, want %q", got.IPAM, NetworkIPAMModeExternal)
	}
	if got.RouteAdvertisementsRef != nil {
		t.Errorf("RouteAdvertisementsRef = %+v after round-trip, want nil", got.RouteAdvertisementsRef)
	}
}

// TestNetworkConfigEnumValues pins the exact issue #26 enum values.
func TestNetworkConfigEnumValues(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"NetworkAttachmentKindNetworkAttachmentDefinition", string(NetworkAttachmentKindNetworkAttachmentDefinition), "NetworkAttachmentDefinition"},
		{"NetworkAttachmentKindClusterUserDefinedNetwork", string(NetworkAttachmentKindClusterUserDefinedNetwork), "ClusterUserDefinedNetwork"},
		{"NetworkTopologyLocalnet", string(NetworkTopologyLocalnet), "Localnet"},
		{"NetworkTopologyLayer2", string(NetworkTopologyLayer2), "Layer2"},
		{"NetworkTopologyLayer3", string(NetworkTopologyLayer3), "Layer3"},
		{"NetworkRoleSecondary", string(NetworkRoleSecondary), "Secondary"},
		{"NetworkRolePrimary", string(NetworkRolePrimary), "Primary"},
		{"NetworkExposureLayer2", string(NetworkExposureLayer2), "Layer2"},
		{"NetworkExposureBGP", string(NetworkExposureBGP), "BGP"},
		{"NetworkIPAMModeDHCP", string(NetworkIPAMModeDHCP), "DHCP"},
		{"NetworkIPAMModeExternal", string(NetworkIPAMModeExternal), "External"},
		{"NetworkIPAMModeOVNKubernetes", string(NetworkIPAMModeOVNKubernetes), "OVNKubernetes"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

// TestNetworkConfigCRDSchema verifies the generated Infra CRD schema: enum
// constraints on each public enum, gateway no longer unconditionally required,
// and the mixed-form CEL validations covering every structured field.
func TestNetworkConfigCRDSchema(t *testing.T) {
	data, err := os.ReadFile("../../config/crd/bases/hostedcluster.densityops.com_infras.yaml")
	if err != nil {
		t.Fatalf("read generated CRD: %v", err)
	}
	var crd map[string]any
	if err := yaml.Unmarshal(data, &crd); err != nil {
		t.Fatalf("parse generated CRD: %v", err)
	}

	networkConfig := crd["spec"].(map[string]any)["versions"].([]any)[0].(map[string]any)["schema"].(map[string]any)["openAPIV3Schema"].(map[string]any)["properties"].(map[string]any)["spec"].(map[string]any)["properties"].(map[string]any)["networkConfig"].(map[string]any)

	required := stringSlice(t, networkConfig["required"])
	if slices.Contains(required, "gateway") {
		t.Errorf("networkConfig.required still contains gateway: %v", required)
	}
	if !slices.Contains(required, "cidr") {
		t.Errorf("networkConfig.required must contain cidr: %v", required)
	}

	props := networkConfig["properties"].(map[string]any)
	enumChecks := []struct {
		field string
		want  []string
	}{
		{"topology", []string{"Localnet", "Layer2", "Layer3"}},
		{"role", []string{"Secondary", "Primary"}},
		{"exposure", []string{"Layer2", "BGP"}},
		{"ipam", []string{"DHCP", "External", "OVNKubernetes"}},
	}
	for _, ec := range enumChecks {
		got := stringSlice(t, props[ec.field].(map[string]any)["enum"])
		if !slices.Equal(got, ec.want) {
			t.Errorf("networkConfig.%s enum = %v, want %v", ec.field, got, ec.want)
		}
	}
	if got := stringSlice(t, props["attachment"].(map[string]any)["properties"].(map[string]any)["kind"].(map[string]any)["enum"]); !slices.Equal(got, []string{"NetworkAttachmentDefinition", "ClusterUserDefinedNetwork"}) {
		t.Errorf("attachment.kind enum = %v", got)
	}

	validations := networkConfig["x-kubernetes-validations"].([]any)
	if len(validations) < 4 {
		t.Fatalf("x-kubernetes-validations has %d rules, want >= 4", len(validations))
	}
	rules := make([]string, 0, len(validations))
	for _, v := range validations {
		rules = append(rules, v.(map[string]any)["rule"].(string))
	}
	for _, want := range []string{
		"has(self.attachment) || has(self.networkAttachmentDefinition)",
		`!( (has(self.networkAttachmentDefinition) || has(self.networkAttachmentNamespace)) && (has(self.attachment) || has(self.topology) || has(self.role) || has(self.exposure) || has(self.ipam) || has(self.routeAdvertisementsRef)) )`,
		`has(self.attachment) || (!has(self.topology) && !has(self.role) && !has(self.exposure) && !has(self.ipam) && !has(self.routeAdvertisementsRef))`,
		`!has(self.networkAttachmentNamespace) || has(self.networkAttachmentDefinition)`,
	} {
		if !slices.Contains(rules, want) {
			t.Errorf("missing CEL rule %q in %v", want, rules)
		}
	}
}

func stringSlice(t *testing.T, v any) []string {
	t.Helper()
	items, ok := v.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", v)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.(string))
	}
	return out
}
