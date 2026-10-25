package config

import (
	"strings"
	"testing"
)

// TestDefaultPortsDistinct: a config that omits every server section must
// validate and resolve to three different ports.
func TestDefaultPortsDistinct(t *testing.T) {
	var c Config
	if err := c.Validate(); err != nil {
		t.Fatalf("empty config: %v", err)
	}
	b, r, m := c.BalancerConfig().Port, c.RegistryConfig().Port, c.MetricsConfig().Port
	if b == r || b == m || r == m {
		t.Fatalf("default ports collide: balancer=%d registry=%d metrics=%d", b, r, m)
	}
}

func TestProxyBodyLimitDefault(t *testing.T) {
	if got := (&Config{}).ProxyBodyLimit(); got != 16<<10 {
		t.Fatalf("unset body-limit = %d, want %d", got, 16<<10)
	}
	if got := (&Config{Proxy: ProxySettings{BodyLimit: 1024}}).ProxyBodyLimit(); got != 1024 {
		t.Fatalf("explicit body-limit = %d, want 1024", got)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := map[string]struct {
		cfg  Config
		want string
	}{
		"duplicate explicit port": {
			cfg: Config{
				Balancer:  BalancerSettings{Server: ServerSettings{Port: 9000}},
				Discovery: DiscoverySettings{Registry: RegistrySettings{Server: ServerSettings{Port: 9000}}},
			},
			want: "discovery.registry.server.port 9000 is already used by balancing.server",
		},
		"explicit port clashes with a default": {
			cfg:  Config{Observability: ObservabilitySettings{Metrics: MetricsSettings{Server: ServerSettings{Port: DefaultBalancerPort}}}},
			want: "observability.metrics.server.port 8080 is already used by balancing.server",
		},
		"negative health interval": {
			cfg:  Config{Health: HealthSettings{Interval: "-5s"}},
			want: `invalid health.interval "-5s": must not be negative`,
		},
		"negative probe timeout": {
			cfg:  Config{Health: HealthSettings{Client: HealthClientSettings{Timeout: "-1s"}}},
			want: `invalid health.client.timeout "-1s": must not be negative`,
		},
		"negative server timeout": {
			cfg:  Config{Balancer: BalancerSettings{Server: ServerSettings{WriteTimeout: "-1s"}}},
			want: `invalid balancing.server.write-timeout "-1s": must not be negative`,
		},
		"negative body limit": {
			cfg:  Config{Proxy: ProxySettings{BodyLimit: -1}},
			want: "invalid proxy.body-limit -1: must be >= 0",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.want)
			}
		})
	}
}
