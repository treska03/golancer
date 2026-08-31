package balancer

import (
	"sync/atomic"

	"github.com/treska03/golancer/internal/domain"
)

type RoundRobinSelector struct {
	reg     Registry
	current atomic.Uint32
}

func NewRoundRobinSelector(r Registry) *RoundRobinSelector {
	return &RoundRobinSelector{reg: r}
}

// GetServer selects the next healthy URL using Round Robin.
//
// FIXME: Round Robin fails its 'fairness' when there are multiple backends down.
func (rrs *RoundRobinSelector) GetServer() (*domain.Backend, error) {
	backends := rrs.reg.ListBackends()
	n := uint32(len(backends))
	if n == 0 {
		return nil, ErrNoBackends
	}
	start := rrs.current.Add(1) - 1
	for i := range n {
		b := backends[(start+i)%n]
		if b.Healthy() {
			return b, nil
		}
	}
	return nil, ErrNoBackends
}
