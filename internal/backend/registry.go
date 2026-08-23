package backend

import (
	"fmt"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/treska03/golancer/internal/domain"
)

// Registry holds the set of backends and supports concurrent registration,
// deregistration, and listing.
//
// Reads are lock-free via copy-on-write: the backend slice is replaced
// wholesale on every mutation and never modified in place, so the slice
// returned by ListBackends is safe to read concurrently and must be treated as
// read-only by callers.
type Registry struct {
	mu       sync.Mutex // serializes writers with each other
	backends atomic.Pointer[[]*domain.Backend]
}

func NewRegistry(backends []*domain.Backend) *Registry {
	r := &Registry{}
	r.backends.Store(&backends)
	return r
}

// ListBackends returns the current set of backends. The returned slice must not
// be modified by the caller.
func (r *Registry) ListBackends() []*domain.Backend {
	if p := r.backends.Load(); p != nil {
		return *p
	}
	return nil
}

// AddNewBackend registers a new backend for the given URL and returns its
// generated instance ID. It fails if the URL is already registered.
func (r *Registry) AddNewBackend(u *url.URL) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cur := r.ListBackends()
	if slices.ContainsFunc(cur, func(el *domain.Backend) bool { return el.URL.String() == u.String() }) {
		return "", fmt.Errorf("url %s is already registered", u.String())
	}

	b := domain.NewBackend(u)
	updated := append(slices.Clone(cur), b)
	r.backends.Store(&updated)
	return b.InstanceID, nil
}

// RemoveInstanceByID deregisters the backend with the given instance ID.
//
// Returns true if instanceID was present in backends list
func (r *Registry) RemoveInstanceByID(instanceID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	ok := slices.ContainsFunc(*r.backends.Load(), func(el *domain.Backend) bool {
		return el.InstanceID == instanceID
	})
	if !ok {
		return false
	}

	updated := slices.DeleteFunc(slices.Clone(r.ListBackends()), func(b *domain.Backend) bool {
		return b.InstanceID == instanceID
	})
	r.backends.Store(&updated)
	return true
}
