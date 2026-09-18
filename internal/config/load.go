package config

import (
	"fmt"
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

// Load reads and parses the YAML config file at path and validates it.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %q: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

// DomainBackends parses configured URLs into domain.Backend structures.
func (c *Config) DomainBackends() []*domain.Backend {
	backends := make([]*domain.Backend, 0, len(c.Backends))
	for _, backend := range c.Backends {
		u, _ := url.Parse(backend.URL)
		w := backend.Weight
		if w == 0 {
			w = 1
		}
		backends = append(backends, domain.NewBackend(u, w))
	}
	return backends
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

// HealthConfig converts the YAML health settings into health.Config.
func (c *Config) HealthConfig() *health.Config {
	out := &health.Config{
		Path:               c.Health.Client.Path,
		HealthyThreshold:   c.Health.HealthyThreshold,
		UnhealthyThreshold: c.Health.UnhealthyThreshold,
		MaxConcurrent:      c.Health.MaxConcurrent,
	}
	if s := c.Health.Interval; s != "" {
		out.Interval, _ = time.ParseDuration(s)
	}
	if s := c.Health.Client.Timeout; s != "" {
		out.Timeout, _ = time.ParseDuration(s)
	}
	return out
}

func (c *Config) serverConfig(s ServerSettings) *server.Config {
	out := &server.Config{
		Port:         DefaultServerPort,
		ReadTimeout:  DefaultServerReadTimeout,
		WriteTimeout: DefaultServerWriteTimeout,
	}

	if s.Port != 0 {
		out.Port = s.Port
	}
	if s.ReadTimeout != "" {
		out.ReadTimeout, _ = time.ParseDuration(s.ReadTimeout)
	}
	if s.WriteTimeout != "" {
		out.WriteTimeout, _ = time.ParseDuration(s.WriteTimeout)
	}
	return out
}

func (c *Config) BalancerConfig() *server.Config {
	return c.serverConfig(c.Balancer.Server)
}

func (c *Config) RegistryConfig() *server.Config {
	return c.serverConfig(c.Discovery.Registry.Server)
}

func (c *Config) MetricsConfig() *server.Config {
	return c.serverConfig(c.Observability.Metrics.Server)
}
