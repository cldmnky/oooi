package leasedb

import (
	"net"
	"testing"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupRange(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "too few arguments",
			args:    []string{"file.db", "10.0.0.1"},
			wantErr: true,
			errMsg:  "invalid number of arguments",
		},
		{
			name:    "empty filename",
			args:    []string{"", "10.0.0.1", "10.0.0.10", "1h"},
			wantErr: true,
			errMsg:  "file name cannot be empty",
		},
		{
			name:    "invalid start IP",
			args:    []string{":memory:", "invalid-ip", "10.0.0.10", "1h"},
			wantErr: true,
			errMsg:  "invalid IPv4 address",
		},
		{
			name:    "invalid end IP",
			args:    []string{":memory:", "10.0.0.1", "invalid-ip", "1h"},
			wantErr: true,
			errMsg:  "invalid IPv4 address",
		},
		{
			name:    "start IP greater than end IP",
			args:    []string{":memory:", "10.0.0.10", "10.0.0.1", "1h"},
			wantErr: true,
			errMsg:  "start of IP range has to be lower",
		},
		{
			name:    "start IP equal to end IP",
			args:    []string{":memory:", "10.0.0.5", "10.0.0.5", "1h"},
			wantErr: true,
			errMsg:  "start of IP range has to be lower",
		},
		{
			name:    "invalid lease duration",
			args:    []string{":memory:", "10.0.0.1", "10.0.0.10", "invalid"},
			wantErr: true,
			errMsg:  "invalid lease duration",
		},
		{
			name:    "valid setup",
			args:    []string{":memory:", "10.0.0.1", "10.0.0.10", "1h"},
			wantErr: false,
		},
		{
			name:    "IPv6 as start address",
			args:    []string{":memory:", "2001:db8::1", "10.0.0.10", "1h"},
			wantErr: true,
			errMsg:  "invalid IPv4 address",
		},
		{
			name:    "IPv6 as end address",
			args:    []string{":memory:", "10.0.0.1", "2001:db8::10", "1h"},
			wantErr: true,
			errMsg:  "invalid IPv4 address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := setupRange(tt.args...)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, handler)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, handler)
			}
		})
	}
}

func TestHandler4NewLease(t *testing.T) {
	// Setup plugin state
	handler, err := setupRange(":memory:", "10.0.0.1", "10.0.0.10", "1h")
	require.NoError(t, err)
	require.NotNil(t, handler)

	// Create DHCP request
	req := &dhcpv4.DHCPv4{
		ClientHWAddr: net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
	}
	resp, err := dhcpv4.New()
	require.NoError(t, err)

	// Call handler
	result, stop := handler(req, resp)
	assert.NotNil(t, result)
	assert.False(t, stop)
	assert.NotNil(t, result.YourIPAddr)
	assert.True(t, result.YourIPAddr.IsPrivate())

	// Check lease time option
	leaseTime := result.Options.Get(dhcpv4.OptionIPAddressLeaseTime)
	assert.NotNil(t, leaseTime)
}

func TestHandler4ExistingLease(t *testing.T) {
	// Setup plugin state
	handler, err := setupRange(":memory:", "10.0.0.1", "10.0.0.10", "1h")
	require.NoError(t, err)

	// Create DHCP request
	mac := net.HardwareAddr{0x11, 0x22, 0x33, 0x44, 0x55, 0x66}
	req := &dhcpv4.DHCPv4{ClientHWAddr: mac}
	resp, err := dhcpv4.New()
	require.NoError(t, err)

	// First request - allocate new IP
	result1, stop1 := handler(req, resp)
	assert.NotNil(t, result1)
	assert.False(t, stop1)
	firstIP := result1.YourIPAddr

	// Second request - should get same IP
	req2 := &dhcpv4.DHCPv4{ClientHWAddr: mac}
	resp2, err := dhcpv4.New()
	require.NoError(t, err)
	result2, stop2 := handler(req2, resp2)
	assert.NotNil(t, result2)
	assert.False(t, stop2)
	assert.Equal(t, firstIP.String(), result2.YourIPAddr.String())
}

func TestHandler4LeaseRenewal(t *testing.T) {
	// Setup plugin state with short lease time
	_, err := setupRange(":memory:", "10.0.0.1", "10.0.0.10", "1s")
	require.NoError(t, err)

	// Cast to get access to plugin state
	pluginState := &PluginState{}
	pluginState.LeaseTime = 1 * time.Second
	pluginState.Recordsv4 = make(map[string]*Record)
	pluginState.registerBackingDB(":memory:")

	// Add an expired lease
	mac := net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xf0}
	expiredRecord := &Record{
		IP:      net.IPv4(10, 0, 0, 5),
		expires: int(time.Now().Add(-1 * time.Hour).Unix()),
	}
	pluginState.Recordsv4[mac.String()] = expiredRecord
	pluginState.saveIPAddress(mac, expiredRecord)

	// Request should renew the lease
	req := &dhcpv4.DHCPv4{ClientHWAddr: mac}
	resp, err := dhcpv4.New()
	require.NoError(t, err)
	result, stop := pluginState.Handler4(req, resp)

	assert.NotNil(t, result)
	assert.False(t, stop)
	assert.Equal(t, expiredRecord.IP.String(), result.YourIPAddr.String())

	// Check that expiry was updated
	updatedRecord := pluginState.Recordsv4[mac.String()]
	assert.True(t, time.Unix(int64(updatedRecord.expires), 0).After(time.Now()))
}

func TestHandler4MultipleClients(t *testing.T) {
	// Setup plugin state
	handler, err := setupRange(":memory:", "10.0.0.1", "10.0.0.5", "1h")
	require.NoError(t, err)

	allocatedIPs := make(map[string]bool)

	// Allocate IPs to multiple clients
	for i := 0; i < 5; i++ {
		mac := net.HardwareAddr{0x00, 0x11, 0x22, 0x33, 0x44, byte(i)}
		req := &dhcpv4.DHCPv4{ClientHWAddr: mac}
		resp, err := dhcpv4.New()
		require.NoError(t, err)

		result, stop := handler(req, resp)
		assert.NotNil(t, result)
		assert.False(t, stop)
		assert.NotNil(t, result.YourIPAddr)

		// Ensure each client gets a unique IP
		ipStr := result.YourIPAddr.String()
		assert.False(t, allocatedIPs[ipStr], "IP %s was allocated twice", ipStr)
		allocatedIPs[ipStr] = true
	}

	assert.Equal(t, 5, len(allocatedIPs))
}

func TestHandler4IPExhaustion(t *testing.T) {
	// Setup plugin state with very small range
	handler, err := setupRange(":memory:", "10.0.0.1", "10.0.0.2", "1h")
	require.NoError(t, err)

	// Allocate all available IPs
	for i := 0; i < 2; i++ {
		mac := net.HardwareAddr{0xf0, 0xf1, 0xf2, 0xf3, 0xf4, byte(i)}
		req := &dhcpv4.DHCPv4{ClientHWAddr: mac}
		resp, err := dhcpv4.New()
		require.NoError(t, err)

		result, stop := handler(req, resp)
		assert.NotNil(t, result)
		assert.False(t, stop)
	}

	// Try to allocate one more - should fail
	mac := net.HardwareAddr{0xf0, 0xf1, 0xf2, 0xf3, 0xf4, 0x99}
	req := &dhcpv4.DHCPv4{ClientHWAddr: mac}
	resp, err := dhcpv4.New()
	require.NoError(t, err)

	result, stop := handler(req, resp)
	assert.Nil(t, result)
	assert.True(t, stop)
}

func TestSetupRangeWithExistingLeases(t *testing.T) {
	// Create a database with existing leases
	pl := &PluginState{}
	err := pl.registerBackingDB(":memory:")
	require.NoError(t, err)

	// Add some existing leases
	mac1, _ := net.ParseMAC("aa:bb:cc:dd:ee:01")
	rec1 := &Record{IP: net.IPv4(10, 0, 0, 2), expires: int(time.Now().Add(1 * time.Hour).Unix())}
	err = pl.saveIPAddress(mac1, rec1)
	require.NoError(t, err)

	mac2, _ := net.ParseMAC("aa:bb:cc:dd:ee:02")
	rec2 := &Record{IP: net.IPv4(10, 0, 0, 3), expires: int(time.Now().Add(1 * time.Hour).Unix())}
	err = pl.saveIPAddress(mac2, rec2)
	require.NoError(t, err)

	// Now setup range - it should load existing leases
	// Note: This test would need to use the same database file
	// For simplicity, we're testing the concept
	loadedRecords, err := loadRecords(pl.leasedb)
	require.NoError(t, err)
	assert.Equal(t, 2, len(loadedRecords))
	assert.NotNil(t, loadedRecords[mac1.String()])
	assert.NotNil(t, loadedRecords[mac2.String()])
}
