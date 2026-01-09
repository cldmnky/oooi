package kubevirt

import (
	"context"
	"net"
	"testing"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	kubevirtv1 "kubevirt.io/api/core/v1"

	"github.com/cldmnky/oooi/internal/dhcp/plugins/kubevirt/client/versioned/fake"
)

func TestSetupKubevirt(t *testing.T) {
	// Test case 1: Valid argument
	handler, err := setupKubevirt(clientcmd.RecommendedHomeFile)
	assert.NoError(t, err)
	assert.NotNil(t, handler)

	// Test case 2: Invalid argument
	handler, err = setupKubevirt()
	assert.Error(t, err)
	assert.Nil(t, handler)
}

func TestKubevirtHandler4(t *testing.T) {
	k := &KubevirtState{
		Client: fake.NewSimpleClientset(),
	}
	req := &dhcpv4.DHCPv4{
		ClientHWAddr: net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
	}
	resp := &dhcpv4.DHCPv4{}
	// add instance to fake client
	k.Client.KubevirtV1().VirtualMachineInstances("test").Create(context.Background(), &kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Spec: kubevirtv1.VirtualMachineInstanceSpec{},
		Status: kubevirtv1.VirtualMachineInstanceStatus{
			Interfaces: []kubevirtv1.VirtualMachineInstanceNetworkInterface{
				{
					IP:  "10.202.2.2",
					MAC: "00:11:22:33:44:55",
				},
			},
		},
	}, metav1.CreateOptions{})
	expectedResp := resp
	expectedContinue := false
	actualResp, actualContinue := k.kubevirtHandler4(req, resp)
	assert.Equal(t, expectedResp, actualResp)
	assert.Equal(t, expectedContinue, actualContinue)
}

func TestKubevirtHandler4NoMatch(t *testing.T) {
	k := &KubevirtState{
		Client: fake.NewSimpleClientset(),
	}
	req := &dhcpv4.DHCPv4{
		ClientHWAddr: net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
	}
	resp := &dhcpv4.DHCPv4{}

	// Create VM instance with different MAC
	k.Client.KubevirtV1().VirtualMachineInstances("test").Create(context.Background(), &kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vm",
			Namespace: "test",
		},
		Status: kubevirtv1.VirtualMachineInstanceStatus{
			Interfaces: []kubevirtv1.VirtualMachineInstanceNetworkInterface{
				{
					IP:  "10.202.2.3",
					MAC: "00:11:22:33:44:66",
				},
			},
		},
	}, metav1.CreateOptions{})

	actualResp, actualContinue := k.kubevirtHandler4(req, resp)
	assert.Nil(t, actualResp)
	assert.True(t, actualContinue)
}

func TestGetKubevirtInstanceForMAC(t *testing.T) {
	tests := []struct {
		name      string
		instances []KubevirtInstance
		mac       string
		wantName  string
		wantNil   bool
	}{
		{
			name: "found matching MAC",
			instances: []KubevirtInstance{
				{
					Name:      "vm1",
					Namespace: "default",
					Interfaces: []kubevirtv1.VirtualMachineInstanceNetworkInterface{
						{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.1"},
					},
				},
			},
			mac:      "aa:bb:cc:dd:ee:ff",
			wantName: "vm1",
			wantNil:  false,
		},
		{
			name: "MAC not found",
			instances: []KubevirtInstance{
				{
					Name:      "vm1",
					Namespace: "default",
					Interfaces: []kubevirtv1.VirtualMachineInstanceNetworkInterface{
						{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.1"},
					},
				},
			},
			mac:     "11:22:33:44:55:66",
			wantNil: true,
		},
		{
			name: "multiple interfaces, found in second",
			instances: []KubevirtInstance{
				{
					Name:      "vm2",
					Namespace: "default",
					Interfaces: []kubevirtv1.VirtualMachineInstanceNetworkInterface{
						{MAC: "aa:bb:cc:dd:ee:01", IP: "10.0.0.1"},
						{MAC: "aa:bb:cc:dd:ee:02", IP: "10.0.0.2"},
					},
				},
			},
			mac:      "aa:bb:cc:dd:ee:02",
			wantName: "vm2",
			wantNil:  false,
		},
		{
			name:      "empty instances",
			instances: []KubevirtInstance{},
			mac:       "aa:bb:cc:dd:ee:ff",
			wantNil:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &KubevirtState{
				Instances: tt.instances,
			}
			result := k.getKubevirtInstanceForMAC(tt.mac)
			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				assert.NotNil(t, result)
				assert.Equal(t, tt.wantName, result.Name)
			}
		})
	}
}

func TestAddKubevirtInstance(t *testing.T) {
	tests := []struct {
		name           string
		existing       []KubevirtInstance
		newInstance    *KubevirtInstance
		expectedCount  int
		expectedUpdate bool
	}{
		{
			name:     "add new instance to empty list",
			existing: []KubevirtInstance{},
			newInstance: &KubevirtInstance{
				Name:      "vm1",
				Namespace: "default",
			},
			expectedCount: 1,
		},
		{
			name: "update existing instance",
			existing: []KubevirtInstance{
				{
					Name:      "vm1",
					Namespace: "default",
					Interfaces: []kubevirtv1.VirtualMachineInstanceNetworkInterface{
						{MAC: "old:mac:addr"},
					},
				},
			},
			newInstance: &KubevirtInstance{
				Name:      "vm1",
				Namespace: "default",
				Interfaces: []kubevirtv1.VirtualMachineInstanceNetworkInterface{
					{MAC: "new:mac:addr"},
				},
			},
			expectedCount:  1,
			expectedUpdate: true,
		},
		{
			name: "add new instance to existing list",
			existing: []KubevirtInstance{
				{
					Name:      "vm1",
					Namespace: "default",
				},
			},
			newInstance: &KubevirtInstance{
				Name:      "vm2",
				Namespace: "default",
			},
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &KubevirtState{
				Instances: tt.existing,
			}
			k.addKubevirtInstance(tt.newInstance)
			assert.Equal(t, tt.expectedCount, len(k.Instances))

			if tt.expectedUpdate {
				// Verify the instance was updated
				found := false
				for _, inst := range k.Instances {
					if inst.Name == tt.newInstance.Name && inst.Namespace == tt.newInstance.Namespace {
						found = true
						assert.Equal(t, tt.newInstance.Interfaces, inst.Interfaces)
					}
				}
				assert.True(t, found)
			}
		})
	}
}

func TestRefreshKubevirtInstances(t *testing.T) {
	client := fake.NewSimpleClientset()

	// Create multiple VMs in different namespaces
	client.KubevirtV1().VirtualMachineInstances("ns1").Create(context.Background(), &kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm1",
			Namespace: "ns1",
		},
		Status: kubevirtv1.VirtualMachineInstanceStatus{
			Interfaces: []kubevirtv1.VirtualMachineInstanceNetworkInterface{
				{MAC: "aa:bb:cc:dd:ee:01", IP: "10.0.1.1"},
			},
		},
	}, metav1.CreateOptions{})

	client.KubevirtV1().VirtualMachineInstances("ns2").Create(context.Background(), &kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vm2",
			Namespace: "ns2",
		},
		Status: kubevirtv1.VirtualMachineInstanceStatus{
			Interfaces: []kubevirtv1.VirtualMachineInstanceNetworkInterface{
				{MAC: "aa:bb:cc:dd:ee:02", IP: "10.0.2.1"},
			},
		},
	}, metav1.CreateOptions{})

	k := &KubevirtState{
		Client: client,
	}

	err := k.refreshKubevirtInstances()
	assert.NoError(t, err)
	assert.Equal(t, 2, len(k.Instances))

	// Verify instances were added correctly
	foundVM1 := false
	foundVM2 := false
	for _, inst := range k.Instances {
		if inst.Name == "vm1" && inst.Namespace == "ns1" {
			foundVM1 = true
		}
		if inst.Name == "vm2" && inst.Namespace == "ns2" {
			foundVM2 = true
		}
	}
	assert.True(t, foundVM1)
	assert.True(t, foundVM2)
}

func TestKubevirtHandler4WithHostname(t *testing.T) {
	k := &KubevirtState{
		Client: fake.NewSimpleClientset(),
	}

	// Create VM instance
	vmName := "test-vm-hostname"
	k.Client.KubevirtV1().VirtualMachineInstances("default").Create(context.Background(), &kubevirtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vmName,
			Namespace: "default",
		},
		Status: kubevirtv1.VirtualMachineInstanceStatus{
			Interfaces: []kubevirtv1.VirtualMachineInstanceNetworkInterface{
				{MAC: "aa:bb:cc:dd:ee:ff", IP: "10.0.0.1"},
			},
		},
	}, metav1.CreateOptions{})

	req := &dhcpv4.DHCPv4{
		ClientHWAddr: net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
	}
	resp := &dhcpv4.DHCPv4{}

	result, stop := k.kubevirtHandler4(req, resp)
	assert.NotNil(t, result)
	assert.False(t, stop)

	// Verify hostname was set
	hostname := result.HostName()
	assert.Equal(t, vmName, hostname)
}
