// Package health implements active health probing for the backend pool. A
// Prober periodically issues an HTTP probe to every registered backend and
// flips each backend's health flag so the balancer only routes to live ones.
package health

import (
	"context"
	"io"
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/treska03/golancer/internal/domain"
)

// Registry is the slice of the backend registry the prober needs: it reads the
// backends to probe and reports health transitions back. It never mutates the
// backends it lists — the registry owns those writes via SetHealthy.
type Registry interface {
	ListBackends() []*domain.Backend
	SetHealthy(instanceID string, healthy bool) bool
}

// Config controls active health probing. Zero-valued fields fall back to
// sensible defaults (see withDefaults).
type Config struct {
	// Interval is the delay between probe rounds.
	Interval time.Duration
	// Timeout bounds a single probe request.
	Timeout time.Duration
	// Path is the URL path probed on each backend, e.g. "/healthz".
	Path string
	// HealthyThreshold is the number of consecutive successful probes required
	// to (re)admit a backend into rotation.
	HealthyThreshold int
	// UnhealthyThreshold is the number of consecutive failed probes required to
	// evict a backend from rotation.
	UnhealthyThreshold int
	// MaxConcurrent bounds how many backends are probed in parallel per round.
	// Zero means unbounded.
	MaxConcurrent int
}

func (c *Config) withDefaults() *Config {
	if c.Interval == 0 {
		c.Interval = 5 * time.Second
	}
	if c.Timeout == 0 {
		c.Timeout = 2 * time.Second
	}
	if c.Path == "" {
		c.Path = "/healthz"
	}
	if c.HealthyThreshold == 0 {
		c.HealthyThreshold = 1
	}
	if c.UnhealthyThreshold == 0 {
		c.UnhealthyThreshold = 3
	}
	return c
}

// Prober periodically probes every registered backend and updates each
// backend's health flag using consecutive-success / consecutive-failure
// thresholds so a single blip does not flap a backend in or out of rotation.
//
// The streak map is only read or written from the goroutine driving a probe
// round (per-backend probe results are folded back in that goroutine after all
// probes complete), so it needs no synchronization.
type Prober struct {
	reg    Registry
	cfg    *Config
	client *http.Client
	streak map[string]int // instanceID -> signed streak: >0 successes, <0 failures
}

// NewProber builds a Prober for reg using cfg (with defaults applied).
func NewProber(reg Registry, cfg *Config) *Prober {
	cfg = cfg.withDefaults()
	return &Prober{
		reg: reg,
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
			// Health checks should not chase redirects.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		streak: make(map[string]int),
	}
}

// Run probes on cfg.Interval until ctx is cancelled. It does not probe
// immediately; call ProbeOnce first if you need health seeded before serving.
func (p *Prober) Run(ctx context.Context) {
	t := time.NewTicker(p.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.probeRound(ctx, false)
		}
	}
}

// ProbeOnce runs a single probe round synchronously. It is intended to be
// called at startup to seed backend health before the server accepts traffic.
func (p *Prober) ProbeOnce(ctx context.Context) {
	p.probeRound(ctx, true)
}

// probeRound probes every currently-registered backend once and records the
// results against each backend's streak.
func (p *Prober) probeRound(ctx context.Context, initial bool) {
	backends := p.reg.ListBackends()

	// Drop streak state for backends that have left the registry.
	if len(p.streak) > 0 {
		live := make(map[string]struct{}, len(backends))
		for _, b := range backends {
			live[b.InstanceID] = struct{}{}
		}
		for id := range p.streak {
			if _, ok := live[id]; !ok {
				delete(p.streak, id)
			}
		}
	}

	if len(backends) == 0 {
		return
	}

	results := make([]bool, len(backends))
	g, _ := errgroup.WithContext(ctx)
	if p.cfg.MaxConcurrent > 0 {
		g.SetLimit(p.cfg.MaxConcurrent)
	}
	for i, b := range backends {
		g.Go(func() error {
			results[i] = p.probe(ctx, b)
			return nil
		})
	}
	_ = g.Wait() // probe never returns an error; each result is stored in results[i]

	for i, b := range backends {
		p.record(b, results[i], initial)
	}
}

// probe issues a single GET to the backend's health path and reports whether it
// answered with a 2xx within the timeout.
func (p *Prober) probe(ctx context.Context, b *domain.Backend) bool {
	ctx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
	defer cancel()

	target := b.URL.JoinPath(p.cfg.Path).String()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return false
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	// Drain a bounded amount so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// record folds a single probe result into the backend's streak and reports a
// health transition to the registry the moment the streak first reaches a
// threshold. Reporting is left to the registry (the owner of the backend) so
// the prober never writes through the slice it read from ListBackends. The
// streak is clamped at each threshold, so the report fires once per transition
// rather than on every probe.
func (p *Prober) record(b *domain.Backend, ok, initial bool) {
	s := p.streak[b.InstanceID]
	if ok {
		if s < 0 {
			s = 0 // a success resets an in-progress failure streak
		}
		if s < p.cfg.HealthyThreshold {
			s++
			if s == p.cfg.HealthyThreshold || initial {
				p.reg.SetHealthy(b.InstanceID, true)
			}
		}
	} else {
		if s > 0 {
			s = 0 // a failure resets an in-progress success streak
		}
		if s > -p.cfg.UnhealthyThreshold {
			s--
			if s == -p.cfg.UnhealthyThreshold {
				p.reg.SetHealthy(b.InstanceID, false)
			}
		}
	}
	p.streak[b.InstanceID] = s
}
