package metrics

import "fmt"

type EndpointConfig struct {
	Enabled       bool   `json:"enabled"`
	ListenAddress string `json:"listen_address"`
	Path          string `json:"path"`
}

func DefaultEndpoint(listenAddress string) EndpointConfig {
	return EndpointConfig{
		Enabled:       false,
		ListenAddress: listenAddress,
		Path:          "/metrics",
	}
}

func NormalizeEndpoint(cfg EndpointConfig, defaultListenAddress string) EndpointConfig {
	if cfg.ListenAddress == "" {
		cfg.ListenAddress = defaultListenAddress
	}
	if cfg.Path == "" {
		cfg.Path = "/metrics"
	}
	return cfg
}

func ValidateEndpoint(scope string, cfg EndpointConfig) error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.ListenAddress == "" {
		return fmt.Errorf("%s.listen_address must not be empty when enabled", scope)
	}
	if cfg.Path == "" {
		return fmt.Errorf("%s.path must not be empty when enabled", scope)
	}
	if cfg.Path[0] != '/' {
		return fmt.Errorf("%s.path must start with /", scope)
	}
	return nil
}
