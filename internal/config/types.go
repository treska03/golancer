package config

import (
	"time"
)

const (
	DiscoverySelfRegistration = "self-registration"
	DiscoveryKubernetes       = "kubernetes-discovery"
)

const (
	StrategyIPHash           = "ip-hash"
	StrategyLeastConnections = "least-connections"
	StrategyRandom           = "random"
	StrategyRoundRobin       = "round-robin"
)

// Server defaults, used when the corresponding config.yaml fields are omitted.
const (
	DefaultBalancerPort       = 8080
	DefaultRegistryPort       = 8081
	DefaultMetricsPort        = 2112
	DefaultServerReadTimeout  = 5 * time.Second
	DefaultServerWriteTimeout = 10 * time.Second
	DefaultProxyBodyLimit     = 16 << 10
)

type (
	Config struct {
		Backends      []BackendSettings     `yaml:"backends"`
		Balancer      BalancerSettings      `yaml:"balancing"`
		Health        HealthSettings        `yaml:"health"`
		Discovery     DiscoverySettings     `yaml:"discovery"`
		Observability ObservabilitySettings `yaml:"observability"`
		Proxy         ProxySettings         `yaml:"proxy"`
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
		Mode       string             `yaml:"mode"`
		Registry   RegistrySettings   `yaml:"registry"`
		Kubernetes KubernetesSettings `yaml:"kubernetes"`
	}

	// KubernetesSettings configures mode "kubernetes-discovery": the ready pod
	// endpoints of one Service are watched via its EndpointSlices.
	KubernetesSettings struct {
		// Service whose pods are balanced. Required.
		Service string `yaml:"service"`
		// Namespace of the Service. Defaults to the pod's own namespace.
		Namespace string `yaml:"namespace"`
		// PortName is the Service port (by name) to send traffic to. Empty
		// selects the Service's unnamed port.
		PortName string `yaml:"port-name"`
		// APIServer overrides in-cluster auth, e.g. http://127.0.0.1:8001
		// served by `kubectl proxy`, for running outside a cluster.
		APIServer string `yaml:"api-server"`
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

	ProxySettings struct {
		BodyLimit int `yaml:"body-limit"`
		// TrustedProxies lists CIDRs (or bare IPs) of reverse proxies in front
		// of the balancer whose X-Forwarded-For / X-Forwarded-Proto are believed.
		TrustedProxies []string `yaml:"trusted-proxies"`
	}
)
