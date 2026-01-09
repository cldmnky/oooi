package dhcp

type Config struct {
	ConfigFile *string
}

func NewConfig(configFile string) *Config {
	return &Config{
		ConfigFile: &configFile,
	}
}
