package main

import (
	"fmt"
	"log"
	"log/slog"
	"net/url"
	"os"
	"time"

	"github.com/treska03/golancer/internal/backend"
	"github.com/treska03/golancer/internal/balancer"
	"github.com/treska03/golancer/internal/domain"
	"github.com/treska03/golancer/internal/health"
	"github.com/treska03/golancer/internal/proxy"
	"github.com/treska03/golancer/internal/server"
	"gopkg.in/yaml.v3"
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
	DefaultServerMaxRetries   = 3
)

// loadConfig reads config.yaml or exits the process on failure.
func loadConfig(path string) *Config {
	cfg, err := Load(path)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	return cfg
}

// TODO: move maxRetries to cfg
// Config is the top-level configuration loaded from config.yaml. Backends is a
// list of backend URLs; each is assigned a generated instance ID at load time.
type Config struct {
	Balancer BalancerSettings  `yaml:"balancing"`
	Backends []BackendSettings `yaml:"backends"`
	Health   HealthSettings    `yaml:"health"`
	Server   ServerSettings    `yaml:"server"`
}

type BalancerSettings struct {
	Strategy string `yaml:"strategy"`
}

type BackendSettings struct {
	URL    string `yaml:"url"`
	Weight uint64 `yaml:"weight"`
}

// HealthSettings are healthcheck specific settings.
type HealthSettings struct {
	Interval           string `yaml:"interval"`
	Timeout            string `yaml:"timeout"`
	Path               string `yaml:"path"`
	HealthyThreshold   int    `yaml:"healthy-threshold"`
	UnhealthyThreshold int    `yaml:"unhealthy-threshold"`
	MaxConcurrent      int    `yaml:"max-concurrent"`
}

// ServerSettings configure the proxy's HTTP listener. All fields are
// optional; omitted fields fall back to the Default* constants above.
type ServerSettings struct {
	Port         int    `yaml:"port"`
	ReadTimeout  string `yaml:"read-timeout"`
	WriteTimeout string `yaml:"write-timeout"`
	MaxRetries   *int   `yaml:"max-retries"`
}

// Load reads and parses the YAML config file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %q: %w", path, err)
	}

	return &cfg, nil
}

// DomainBackends parses the configured backend URLs into a slice of
// domain.Backend, each with a generated instance ID, ready to seed the
// balancer's pool.
func (c *Config) DomainBackends() ([]*domain.Backend, error) {
	backends := make([]*domain.Backend, 0, len(c.Backends))
	for i, backend := range c.Backends {
		u, err := url.Parse(backend.URL)
		if err != nil {
			return nil, fmt.Errorf("backends.%d invalid url %q in: %w", i, backend.URL, err)
		}
		if u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("backends.%d url %q must include a scheme and host", i, backend.URL)
		}
		w := backend.Weight
		// TODO: this ode looks like a garbage
		if w == 0 {
			w = 1
		}
		backends = append(backends, domain.NewBackend(u, w))
	}
	return backends, nil
}

func (c *Config) ProxySelector(reg *backend.Registry) proxy.Balancer {
	switch c.Balancer.Strategy {
	case StrategyIPHash:
		return proxy.ByClientIP(balancer.NewHashSelector(reg))
	case StrategyLeastConnections:
		return proxy.Rotate(balancer.NewLeastConnectionsSelector(reg))
	case StrategyRandom:
		return proxy.Rotate(balancer.NewRandomSelector(reg))
	case StrategyRoundRobin:
		return proxy.Rotate(balancer.NewRoundRobinSelector(reg))
	}
	slog.Info("unknown balancing strategy: defaulting to round-robin", "strategy", c.Balancer.Strategy)
	return proxy.Rotate(balancer.NewRoundRobinSelector(reg))
}

// HealthConfig converts the YAML health settings into a health.Config.
func (c *Config) HealthConfig() (*health.Config, error) {
	out := &health.Config{
		Path:               c.Health.Path,
		HealthyThreshold:   c.Health.HealthyThreshold,
		UnhealthyThreshold: c.Health.UnhealthyThreshold,
		MaxConcurrent:      c.Health.MaxConcurrent,
	}
	if s := c.Health.Interval; s != "" {
		d, err := time.ParseDuration(s)
		if err != nil {
			return nil, fmt.Errorf("invalid health.interval %q: %w", s, err)
		}
		out.Interval = d
	}
	if s := c.Health.Timeout; s != "" {
		d, err := time.ParseDuration(s)
		if err != nil {
			return nil, fmt.Errorf("invalid health.timeout %q: %w", s, err)
		}
		out.Timeout = d
	}
	return out, nil
}

// ServerConfig converts the YAML server settings into the values needed to
// construct the proxy's http.Server, applying defaults for any omitted
// fields. This replaces the hardcoded values previously passed to New.
type ServerConfig struct {
	Addr         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	MaxRetries   int
}

func (c *Config) ServerConfig() (*server.Config, error) {
	out := &server.Config{
		Port:         DefaultServerPort,
		ReadTimeout:  DefaultServerReadTimeout,
		WriteTimeout: DefaultServerWriteTimeout,
		MaxRetries:   DefaultServerMaxRetries,
	}

	if a := c.Server.Port; a != 0 {
		out.Port = a
	}
	if s := c.Server.ReadTimeout; s != "" {
		d, err := time.ParseDuration(s)
		if err != nil {
			return nil, fmt.Errorf("invalid server.read-timeout %q: %w", s, err)
		}
		out.ReadTimeout = d
	}
	if s := c.Server.WriteTimeout; s != "" {
		d, err := time.ParseDuration(s)
		if err != nil {
			return nil, fmt.Errorf("invalid server.write-timeout %q: %w", s, err)
		}
		out.WriteTimeout = d
	}
	if c.Server.MaxRetries != nil {
		if *c.Server.MaxRetries < 0 {
			return nil, fmt.Errorf("server.max-retries must be >= 0, got %d", *c.Server.MaxRetries)
		}
		out.MaxRetries = *c.Server.MaxRetries
	}

	return out, nil
}
