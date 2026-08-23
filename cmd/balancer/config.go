package main

import (
	"fmt"
	"log"
	"net/url"
	"os"

	"github.com/treska03/golancer/internal/domain"
	"gopkg.in/yaml.v3"
)

func loadBackends() []*domain.Backend {
	cfg, err := Load("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	backends, err := cfg.DomainBackends()
	if err != nil {
		log.Fatalf("Failed to parse backends: %v", err)
	}

	return backends
}

// Config is the top-level configuration loaded from config.yaml. Backends is a
// list of backend URLs; each is assigned a generated instance ID at load time.
type Config struct {
	Backends []string `yaml:"backends"`
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
	for _, raw := range c.Backends {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid url %q: %w", raw, err)
		}
		if u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("url %q must include a scheme and host", raw)
		}
		backends = append(backends, domain.NewBackend(u))
	}
	return backends, nil
}
