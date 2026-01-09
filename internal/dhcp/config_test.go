package dhcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewConfig(t *testing.T) {
	tests := []struct {
		name       string
		configFile string
	}{
		{
			name:       "basic config file",
			configFile: "test.yaml",
		},
		{
			name:       "config with path",
			configFile: "/etc/dhcp/config.yaml",
		},
		{
			name:       "empty config file",
			configFile: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewConfig(tt.configFile)
			assert.NotNil(t, cfg)
			assert.NotNil(t, cfg.ConfigFile)
			assert.Equal(t, tt.configFile, *cfg.ConfigFile)
		})
	}
}

func TestConfigFilePointer(t *testing.T) {
	configFile := "test.yaml"
	cfg := NewConfig(configFile)

	// Verify that ConfigFile is a pointer and points to the right value
	assert.NotNil(t, cfg.ConfigFile)
	assert.Equal(t, configFile, *cfg.ConfigFile)

	// Modify the original string shouldn't affect the config
	configFile = "modified.yaml"
	assert.Equal(t, "test.yaml", *cfg.ConfigFile)
}
