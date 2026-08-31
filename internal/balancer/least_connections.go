package balancer

import (
	"cmp"
	"slices"

	"github.com/treska03/golancer/internal/domain"
)

type LeastConnectionsSelector struct {
	reg HealthyRegistry
}

func NewLeastConnectionsSelector(r HealthyRegistry) *LeastConnectionsSelector {
	return &LeastConnectionsSelector{reg: r}
}

// GetServer returns the backend with the fewest active connections relative to its weight.
func (lcs *LeastConnectionsSelector) GetServer() (*domain.Backend, error) {
	backends := lcs.reg.ListHealthyBackends()
	if len(backends) == 0 {
		return nil, ErrNoBackends
	}
	return slices.MinFunc(backends, func(a, b *domain.Backend) int {
		// Order by load ratio connsA/weightA vs connsB/weightB, compared via
		// cross-multiplication to avoid floating point. cmp.Compare is used
		// rather than a subtraction so unsigned wraparound can't flip the sign.
		aConns := uint64(a.ActiveConnectionsNo.Load() + 1)
		bConns := uint64(b.ActiveConnectionsNo.Load() + 1)
		return cmp.Compare(aConns*b.Weight, bConns*a.Weight)
	}), nil
}
