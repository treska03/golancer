package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"time"

	"github.com/treska03/golancer/internal/domain"
	"github.com/treska03/golancer/internal/health"
	"gopkg.in/yaml.v3"
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
	Backends []BackendSettings `yaml:"backends"`
	Health   HealthSettings    `yaml:"health"`
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
	HealthyThreshold   int    `yaml:"healthyThreshold"`
	UnhealthyThreshold int    `yaml:"unhealthyThreshold"`
	MaxConcurrent      int    `yaml:"maxConcurrent"`
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
