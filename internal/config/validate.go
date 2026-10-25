package config

import (
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/treska03/golancer/internal/domain"
)

// Validate collects all invalid values in the Config and returns them as a single error.
// If valid, it returns nil.
func (c *Config) Validate() error {
	var errs []error

	// Balancer Strategy Validation
	switch c.Balancer.Strategy {
	case StrategyIPHash, StrategyLeastConnections, StrategyRandom, StrategyRoundRobin, "":
		// Valid
	default:
		errs = append(errs, fmt.Errorf("unknown balancing strategy %q", c.Balancer.Strategy))
	}

	for i, backend := range c.Backends {
		u, err := url.Parse(backend.URL)
		if err != nil {
			errs = append(errs, fmt.Errorf("backends.%d invalid url %q: %w", i, backend.URL, err))
		} else if u.Scheme == "" || u.Host == "" {
			errs = append(errs, fmt.Errorf("backends.%d url %q must include a scheme and host", i, backend.URL))
		} else if u.Scheme != "http" && u.Scheme != "https" {
			errs = append(errs, fmt.Errorf("backends.%d url %q must use http or https", i, backend.URL))
		}
		if backend.Weight > domain.MaxWeight {
			errs = append(errs, fmt.Errorf("backends.%d weight %d exceeds maximum %d", i, backend.Weight, domain.MaxWeight))
		}
	}

	// Health Validation
	// validateDuration accepts an empty (unset) value; otherwise it must parse
	// and be non-negative. Negative values would otherwise slip through:
	// time.NewTicker panics on them, and a negative timeout means none at all.
	validateDuration := func(field, s string) {
		if s == "" {
			return
		}
		d, err := time.ParseDuration(s)
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid %s %q: %w", field, s, err))
		} else if d < 0 {
			errs = append(errs, fmt.Errorf("invalid %s %q: must not be negative", field, s))
		}
	}

	validateDuration("health.interval", c.Health.Interval)
	validateDuration("health.client.timeout", c.Health.Client.Timeout)
	if c.Health.HealthyThreshold < 0 {
		errs = append(errs, fmt.Errorf("invalid health.healthy-threshold %d: must be >= 0", c.Health.HealthyThreshold))
	}
	if c.Health.UnhealthyThreshold < 0 {
		errs = append(errs, fmt.Errorf("invalid health.unhealthy-threshold %d: must be >= 0", c.Health.UnhealthyThreshold))
	}
	if c.Health.MaxConcurrent < 0 {
		errs = append(errs, fmt.Errorf("invalid health.max-concurrent %d: must be >= 0", c.Health.MaxConcurrent))
	}

	// Server Settings Validation
	// usedPorts maps each effective (post-default) port to the server that
	// claimed it, so two servers resolving to the same port fail validation
	// instead of crashing with "address already in use" at startup.
	usedPorts := map[int]string{}
	validateServer := func(prefix string, s ServerSettings, defaultPort int) {
		if s.Port < 0 || s.Port > 65535 {
			errs = append(errs, fmt.Errorf("invalid %s.port %d (must be 0-65535)", prefix, s.Port))
		} else if p := s.portOr(defaultPort); usedPorts[p] != "" {
			errs = append(errs, fmt.Errorf("%s.port %d is already used by %s", prefix, p, usedPorts[p]))
		} else {
			usedPorts[p] = prefix
		}
		validateDuration(prefix+".read-timeout", s.ReadTimeout)
		validateDuration(prefix+".write-timeout", s.WriteTimeout)
	}

	validateServer("balancing.server", c.Balancer.Server, DefaultBalancerPort)
	validateServer("discovery.registry.server", c.Discovery.Registry.Server, DefaultRegistryPort)
	validateServer("observability.metrics.server", c.Observability.Metrics.Server, DefaultMetricsPort)

	if c.Balancer.MaxRetries < 0 {
		errs = append(errs, fmt.Errorf("invalid balancing.max-retries %d: must be >= 0", c.Balancer.MaxRetries))
	}

	if c.Proxy.BodyLimit < 0 {
		errs = append(errs, fmt.Errorf("invalid proxy.body-limit %d: must be >= 0", c.Proxy.BodyLimit))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}
