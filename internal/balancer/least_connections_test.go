package balancer

import (
	"testing"

	"github.com/treska03/golancer/internal/domain"
)

func TestLeastConnectionsEmpty(t *testing.T) {
	lcs := NewLeastConnectionsSelector(&fakeHealthyReg{})
	if _, err := lcs.GetServer(); err != ErrNoBackends {
		t.Fatalf("expected ErrNoBackends, got %v", err)
	}
}

// TestLeastConnectionsFewestWins: with equal weights, the backend with fewer
// active connections is chosen.
func TestLeastConnectionsFewestWins(t *testing.T) {
	a := mustBackend(t, "http://a.invalid", 1)
	b := mustBackend(t, "http://b.invalid", 1)
	a.ActiveConnectionsNo.Store(5)
	b.ActiveConnectionsNo.Store(2)

	lcs := NewLeastConnectionsSelector(&fakeHealthyReg{backends: []*domain.Backend{a, b}})
	got, err := lcs.GetServer()
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if got != b {
		t.Fatalf("expected b (2 conns) over a (5 conns), got %v", got.URL)
	}
}

// TestLeastConnectionsWeighted: b has more raw connections than a but three
// times the weight, so its load ratio (3/3=1) is lower than a's (2/1=2) and it
// should be selected. This is the case the old subtraction-based comparator got
// backwards.
func TestLeastConnectionsWeighted(t *testing.T) {
	a := mustBackend(t, "http://a.invalid", 1)
	b := mustBackend(t, "http://b.invalid", 3)
	a.ActiveConnectionsNo.Store(2)
	b.ActiveConnectionsNo.Store(3)

	lcs := NewLeastConnectionsSelector(&fakeHealthyReg{backends: []*domain.Backend{a, b}})
	got, err := lcs.GetServer()
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if got != b {
		t.Fatalf("expected higher-weight b (lower load ratio) over a, got %v", got.URL)
	}
}
