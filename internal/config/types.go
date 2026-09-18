package config

import (
	"time"
)

const (
	StrategyIPHash           = "ip-hash"
	StrategyLeastConnections = "least-connections"
	StrategyRandom           = "random"
	StrategyRoundRobin       = "round-robin"
)

// Server defaults, used when the corresponding config.yaml fields are omitted.
const (
	DefaultServerPort         = 8080
	DefaultServerReadTimeout  = 5 * time.Second
	DefaultServerWriteTimeout = 10 * time.Second
)

type (
	Config struct {
		Backends      []BackendSettings     `yaml:"backends"`
		Balancer      BalancerSettings      `yaml:"balancing"`
		Health        HealthSettings        `yaml:"health"`
		Discovery     DiscoverySettings     `yaml:"discovery"`
		Observability ObservabilitySettings `yaml:"observability"`
	}

	BackendSettings struct {
		URL    string `yaml:"url"`
		Weight uint64 `yaml:"weight"`
	}

	BalancerSettings struct {
		Strategy   string         `yaml:"strategy"`
		MaxRetries int            `yaml:"max-retries"`
		Server     ServerSettings `yaml:"server"`
	}

	HealthSettings struct {
		Interval           string               `yaml:"interval"`
		HealthyThreshold   int                  `yaml:"healthy-threshold"`
		UnhealthyThreshold int                  `yaml:"unhealthy-threshold"`
		MaxConcurrent      int                  `yaml:"max-concurrent"`
		Client             HealthClientSettings `yaml:"client"`
	}

	HealthClientSettings struct {
		Timeout string `yaml:"timeout"`
		Path    string `yaml:"path"`
	}

	DiscoverySettings struct {
		Mode     string           `yaml:"mode"`
		Registry RegistrySettings `yaml:"registry"`
	}

	RegistrySettings struct {
		Server ServerSettings `yaml:"server"`
	}

	ObservabilitySettings struct {
		Metrics MetricsSettings `yaml:"metrics"`
	}

	MetricsSettings struct {
		Server ServerSettings `yaml:"server"`
	}

	// ServerSettings holds the HTTP listener settings shared by every
	// server the application exposes.
	ServerSettings struct {
		Port         int    `yaml:"port"`
		ReadTimeout  string `yaml:"read-timeout"`
		WriteTimeout string `yaml:"write-timeout"`
	}
)
