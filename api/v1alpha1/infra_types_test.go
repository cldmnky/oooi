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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestInfraSpec_Validation(t *testing.T) {
	tests := []struct {
		name    string
		spec    InfraSpec
		wantErr bool
	}{
		{
			name: "valid spec with all fields",
			spec: InfraSpec{
				NetworkConfig: NetworkConfig{
					CIDR:                        "192.168.100.0/24",
					Gateway:                     "192.168.100.1",
					NetworkAttachmentDefinition: "tenant-vlan-100",
				},
				InfraComponents: InfraComponents{
					DHCP: DHCPConfig{
						Enabled:    true,
						RangeStart: "192.168.100.10",
						RangeEnd:   "192.168.100.250",
						ServerIP:   "192.168.100.2",
					},
					DNS: DNSConfig{
						Enabled:  true,
						ServerIP: "192.168.100.3",
					},
					Proxy: ProxyConfig{
						Enabled:  true,
						ServerIP: "192.168.100.4",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "minimal valid spec",
			spec: InfraSpec{
				NetworkConfig: NetworkConfig{
					CIDR:                        "192.168.100.0/24",
					Gateway:                     "192.168.100.1",
					NetworkAttachmentDefinition: "tenant-vlan-100",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			infra := &Infra{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-infra",
					Namespace: "default",
				},
				Spec: tt.spec,
			}

			if infra.Spec.NetworkConfig.CIDR == "" && !tt.wantErr {
				t.Errorf("NetworkConfig.CIDR should not be empty")
			}
		})
	}
}

func TestInfraStatus_Conditions(t *testing.T) {
	tests := []struct {
		name   string
		status InfraStatus
		want   int
	}{
		{
			name: "status with conditions",
			status: InfraStatus{
				Conditions: []metav1.Condition{
					{
						Type:               "Ready",
						Status:             metav1.ConditionTrue,
						Reason:             "ReconciliationSucceeded",
						Message:            "All infrastructure components are ready",
						LastTransitionTime: metav1.Now(),
					},
				},
			},
			want: 1,
		},
		{
			name:   "empty status",
			status: InfraStatus{},
			want:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := len(tt.status.Conditions); got != tt.want {
				t.Errorf("len(Conditions) = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNetworkConfig_Fields(t *testing.T) {
	config := NetworkConfig{
		CIDR:                        "192.168.100.0/24",
		Gateway:                     "192.168.100.1",
		NetworkAttachmentDefinition: "tenant-vlan-100",
	}

	if config.CIDR == "" {
		t.Error("CIDR should be set")
	}
	if config.Gateway == "" {
		t.Error("Gateway should be set")
	}
	if config.NetworkAttachmentDefinition == "" {
		t.Error("NetworkAttachmentDefinition should be set")
	}
}

func TestDHCPConfig_Fields(t *testing.T) {
	dhcp := DHCPConfig{
		Enabled:    true,
		RangeStart: "192.168.100.10",
		RangeEnd:   "192.168.100.250",
	}

	if !dhcp.Enabled {
		t.Error("DHCP should be enabled")
	}
	if dhcp.RangeStart == "" {
		t.Error("RangeStart should be set")
	}
	if dhcp.RangeEnd == "" {
		t.Error("RangeEnd should be set")
	}
}

func TestDNSConfig_Fields(t *testing.T) {
	dns := DNSConfig{
		Enabled:  true,
		ServerIP: "192.168.100.3",
	}

	if !dns.Enabled {
		t.Error("DNS should be enabled")
	}
	if dns.ServerIP == "" {
		t.Error("ServerIP should be set")
	}
}

func TestProxyConfig_Fields(t *testing.T) {
	proxy := ProxyConfig{
		Enabled:  true,
		ServerIP: "192.168.100.4",
	}

	if !proxy.Enabled {
		t.Error("Proxy should be enabled")
	}
	if proxy.ServerIP == "" {
		t.Error("ServerIP should be set")
	}
}
