package balancer

import (
	"net/url"
	"testing"

	"github.com/treska03/golancer/internal/domain"
)

// fakeHealthyReg is a HealthyRegistry that returns a fixed backend set.
type fakeHealthyReg struct {
	backends []*domain.Backend
}

func (f *fakeHealthyReg) ListHealthyBackends() []*domain.Backend { return f.backends }

func mustBackend(t *testing.T, raw string, w uint64) *domain.Backend {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return domain.NewBackend(u, w)
}

func TestRandomSelectorEmpty(t *testing.T) {
	rs := NewRandomSelector(&fakeHealthyReg{})
	if _, err := rs.GetServer(); err != ErrNoBackends {
		t.Fatalf("expected ErrNoBackends, got %v", err)
	}
}

// TestRandomSelectorSingle returns the only backend. Weights are assumed >= 1
// (normalized by config and the register API), so total weight is never 0.
func TestRandomSelectorSingle(t *testing.T) {
	b := mustBackend(t, "http://z.invalid", 1)
	rs := NewRandomSelector(&fakeHealthyReg{backends: []*domain.Backend{b}})
	got, err := rs.GetServer()
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if got != b {
		t.Fatalf("expected the only backend to be returned")
	}
}

// TestRandomSelectorWeightedDistribution checks that selection frequency tracks
// each backend's weight. Tolerances are wide so the statistical test does not
// flake.
func TestRandomSelectorWeightedDistribution(t *testing.T) {
	b1 := mustBackend(t, "http://a.invalid", 1)
	b5 := mustBackend(t, "http://b.invalid", 5)
	rs := NewRandomSelector(&fakeHealthyReg{backends: []*domain.Backend{b1, b5}})

	const n = 200_000
	counts := map[string]int{}
	for range n {
		b, err := rs.GetServer()
		if err != nil {
			t.Fatalf("GetServer: %v", err)
		}
		counts[b.InstanceID]++
	}

	// Expected shares: 1/6 and 5/6.
	got1 := float64(counts[b1.InstanceID]) / n
	got5 := float64(counts[b5.InstanceID]) / n
	if got1 < (1.0/6)*0.85 || got1 > (1.0/6)*1.15 {
		t.Errorf("weight-1 share %.3f outside expected ~0.167", got1)
	}
	if got5 < (5.0/6)*0.97 || got5 > (5.0/6)*1.03 {
		t.Errorf("weight-5 share %.3f outside expected ~0.833", got5)
	}
}
