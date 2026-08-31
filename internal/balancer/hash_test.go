package balancer

import (
	"strconv"
	"testing"

	"github.com/treska03/golancer/internal/domain"
)

func TestHashSelectorEmpty(t *testing.T) {
	hs := NewHashSelector(&fakeHealthyReg{})
	if _, err := hs.GetServerForKey("1.2.3.4"); err != ErrNoBackends {
		t.Fatalf("expected ErrNoBackends, got %v", err)
	}
}

// TestHashSelectorSticky: the same key always maps to the same backend while the
// backend set is stable.
func TestHashSelectorSticky(t *testing.T) {
	backends := []*domain.Backend{
		mustBackend(t, "http://a.invalid", 1),
		mustBackend(t, "http://b.invalid", 1),
		mustBackend(t, "http://c.invalid", 1),
	}
	hs := NewHashSelector(&fakeHealthyReg{backends: backends})

	first, err := hs.GetServerForKey("203.0.113.7")
	if err != nil {
		t.Fatalf("GetServerForKey: %v", err)
	}
	for range 100 {
		got, err := hs.GetServerForKey("203.0.113.7")
		if err != nil {
			t.Fatalf("GetServerForKey: %v", err)
		}
		if got != first {
			t.Fatalf("hash not sticky: got %v then %v", first.URL, got.URL)
		}
	}
}

// TestHashSelectorSpreadsKeys: distinct keys are not all funneled to a single
// backend.
func TestHashSelectorSpreadsKeys(t *testing.T) {
	backends := []*domain.Backend{
		mustBackend(t, "http://a.invalid", 1),
		mustBackend(t, "http://b.invalid", 1),
		mustBackend(t, "http://c.invalid", 1),
	}
	hs := NewHashSelector(&fakeHealthyReg{backends: backends})

	seen := map[string]bool{}
	for i := range 256 {
		key := "198.51.100." + strconv.Itoa(i)
		b, err := hs.GetServerForKey(key)
		if err != nil {
			t.Fatalf("GetServerForKey: %v", err)
		}
		seen[b.InstanceID] = true
	}
	if len(seen) < 2 {
		t.Fatalf("expected keys spread across multiple backends, hit only %d", len(seen))
	}
}
