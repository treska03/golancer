package config

import (
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"strings"
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
		return proxy.ByClientIP(balancer.NewHashSelector(reg), c.TrustedProxies())
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

func (c *Config) serverConfig(s ServerSettings, defaultPort int) *server.Config {
	out := &server.Config{
		Port:         s.portOr(defaultPort),
		ReadTimeout:  DefaultServerReadTimeout,
		WriteTimeout: DefaultServerWriteTimeout,
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
	return c.serverConfig(c.Balancer.Server, DefaultBalancerPort)
}

func (c *Config) RegistryConfig() *server.Config {
	return c.serverConfig(c.Discovery.Registry.Server, DefaultRegistryPort)
}

// portOr returns the configured port, or def when it is unset (0).
func (s ServerSettings) portOr(def int) int {
	if s.Port != 0 {
		return s.Port
	}
	return def
}

func (c *Config) RegistryToken() string {
	return os.Getenv("GOLANCER_REGISTRY_TOKEN")
}

func (c *Config) MetricsConfig() *server.Config {
	return c.serverConfig(c.Observability.Metrics.Server, DefaultMetricsPort)
}

// ProxyBodyLimit returns the max request body size in bytes, falling back to
// DefaultProxyBodyLimit when unset. A zero limit would otherwise make
// http.MaxBytesReader reject every non-empty body.
func (c *Config) ProxyBodyLimit() int64 {
	if c.Proxy.BodyLimit == 0 {
		return DefaultProxyBodyLimit
	}
	return int64(c.Proxy.BodyLimit)
}

// TrustedProxies parses proxy.trusted-proxies. Entries were checked by
// Validate, so unparsable ones can't occur here and are skipped.
func (c *Config) TrustedProxies() proxy.TrustedProxies {
	out := make(proxy.TrustedProxies, 0, len(c.Proxy.TrustedProxies))
	for _, s := range c.Proxy.TrustedProxies {
		if p, err := parsePrefix(s); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// parsePrefix accepts a CIDR ("10.0.0.0/8") or a bare IP ("10.1.2.3"), which
// is treated as a single-address prefix.
func parsePrefix(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		return p.Masked(), err
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	addr = addr.Unmap()
	return netip.PrefixFrom(addr, addr.BitLen()), nil
}
