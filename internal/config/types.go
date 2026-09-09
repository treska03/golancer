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
		Balancer BalancerSettings  `yaml:"balancing"`
		Backends []BackendSettings `yaml:"backends"`
		Health   HealthSettings    `yaml:"health"`
		Server   ServerSettings    `yaml:"server"`
	}

	BalancerSettings struct {
		Strategy string `yaml:"strategy"`
	}

	BackendSettings struct {
		URL    string `yaml:"url"`
		Weight uint64 `yaml:"weight"`
	}

	HealthSettings struct {
		Interval           string `yaml:"interval"`
		Timeout            string `yaml:"timeout"`
		Path               string `yaml:"path"`
		HealthyThreshold   int    `yaml:"healthy-threshold"`
		UnhealthyThreshold int    `yaml:"unhealthy-threshold"`
		MaxConcurrent      int    `yaml:"max-concurrent"`
	}

	ServerSettings struct {
		Balancer BalancerServerSettings `yaml:"balancer"`
		Registry RegistryServerSettings `yaml:"registry"`
	}

	BaseServerSettings struct {
		Port         int    `yaml:"port"`
		ReadTimeout  string `yaml:"read-timeout"`
		WriteTimeout string `yaml:"write-timeout"`
	}

	BalancerServerSettings struct {
		BaseServerSettings `yaml:",inline"`
		MaxRetries         int `yaml:"max-retries"`
	}

	RegistryServerSettings struct {
		BaseServerSettings `yaml:",inline"`
	}
)
