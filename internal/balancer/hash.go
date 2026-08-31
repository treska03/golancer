package balancer

import (
	"hash/fnv"

	"github.com/treska03/golancer/internal/domain"
)

// HashSelector routes by hashing an arbitrary string key to a healthy backend,
// so the same key always maps to the same backend while the healthy set is
// stable (sticky routing / session affinity).
type HashSelector struct {
	reg HealthyRegistry
}

func NewHashSelector(r HealthyRegistry) *HashSelector {
	return &HashSelector{reg: r}
}

// GetServerForKey maps key to a healthy backend. Weights are honored: the hash
// lands in a backend's weight band exactly as in weighted-random selection, just
// keyed off the hash instead of a random draw.
//
// Selection uses plain modulo hashing, so changing the healthy set reshuffles
// most keys. That is the standard hash-routing tradeoff; consistent hashing
// would be needed to minimize disruption.
func (hs *HashSelector) GetServerForKey(key string) (*domain.Backend, error) {
	backends := hs.reg.ListHealthyBackends()
	if len(backends) == 0 {
		return nil, ErrNoBackends
	}

	var total uint64
	for _, b := range backends {
		total += b.Weight
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	pick := uint64(h.Sum32()) % total

	// Walk the backends until pick falls inside a backend's weight band
	// [runningTotal, runningTotal+weight), mirroring RandomSelector.
	for _, b := range backends {
		if pick < b.Weight {
			return b, nil
		}
		pick -= b.Weight
	}

	// Unreachable: pick < total guarantees a match above.
	return backends[len(backends)-1], nil
}
