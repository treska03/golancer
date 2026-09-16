package config

import (
	"errors"
	"fmt"
	"net/url"
	"time"
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

	// Backends Validation
	if len(c.Backends) == 0 {
		errs = append(errs, errors.New("at least one backend must be specified"))
	}
	for i, backend := range c.Backends {
		u, err := url.Parse(backend.URL)
		if err != nil {
			errs = append(errs, fmt.Errorf("backends.%d invalid url %q: %w", i, backend.URL, err))
		} else if u.Scheme == "" || u.Host == "" {
			errs = append(errs, fmt.Errorf("backends.%d url %q must include a scheme and host", i, backend.URL))
		}
	}

	// Health Validation
	if s := c.Health.Interval; s != "" {
		if _, err := time.ParseDuration(s); err != nil {
			errs = append(errs, fmt.Errorf("invalid health.interval %q: %w", s, err))
		}
	}
	if s := c.Health.Timeout; s != "" {
		if _, err := time.ParseDuration(s); err != nil {
			errs = append(errs, fmt.Errorf("invalid health.timeout %q: %w", s, err))
		}
	}
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
	validateBaseServer := func(prefix string, s BaseServerSettings) {
		if s.Port < 0 || s.Port > 65535 {
			errs = append(errs, fmt.Errorf("invalid %s.port %d (must be 0-65535)", prefix, s.Port))
		}
		if s.ReadTimeout != "" {
			if _, err := time.ParseDuration(s.ReadTimeout); err != nil {
				errs = append(errs, fmt.Errorf("invalid %s.read-timeout %q: %w", prefix, s.ReadTimeout, err))
			}
		}
		if s.WriteTimeout != "" {
			if _, err := time.ParseDuration(s.WriteTimeout); err != nil {
				errs = append(errs, fmt.Errorf("invalid %s.write-timeout %q: %w", prefix, s.WriteTimeout, err))
			}
		}
	}

	validateBaseServer("server.balancer", c.Server.Balancer.BaseServerSettings)
	validateBaseServer("server.registry", c.Server.Registry.BaseServerSettings)

	if c.Server.Balancer.MaxRetries < 0 {
		errs = append(errs, fmt.Errorf("invalid server.balancer.max-retries %d: must be >= 0", c.Server.Balancer.MaxRetries))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}
