package balancer

import (
	"math/rand/v2"

	"github.com/treska03/golancer/internal/domain"
)

// RandomSelector picks a healthy backend at random, weighted by each backend's
// Weight: a backend with weight 3 is three times as likely to be chosen as one
// with weight 1. math/rand/v2's top-level generator is safe for concurrent use,
// so the selector needs no locking of its own.
type RandomSelector struct {
	reg HealthyRegistry
}

func NewRandomSelector(r HealthyRegistry) *RandomSelector {
	return &RandomSelector{reg: r}
}

// GetServer returns a healthy backend chosen by weighted random selection.
func (rs *RandomSelector) GetServer() (*domain.Backend, error) {
	backends := rs.reg.ListHealthyBackends()
	if len(backends) == 0 {
		return nil, ErrNoBackends
	}

	var total = uint64(0)
	for _, b := range backends {
		total += b.Weight
	}

	// Pick a point in [0, total) and walk the backends until it lands inside a
	// backend's weight band [runningTotal, runningTotal+weight).
	pick := rand.Uint64N(total)
	for _, b := range backends {
		w := b.Weight
		if pick < w {
			return b, nil
		}
		pick -= w
	}

	// Unreachable: pick < total guarantees a match above. Return the last
	// backend rather than nil so a caller never has to handle both.
	return backends[len(backends)-1], nil
}
