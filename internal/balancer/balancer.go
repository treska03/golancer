package balancer

import (
	"errors"
	"github.com/treska03/golancer/internal/backend"
	"net/url"
	"sync/atomic"
)

var ErrNoBackends = errors.New("no backend servers available")

// ServerPool manages a list of backend target URLs.
type ServerPool struct {
	reg     *backend.Registry
	current atomic.Uint64
}

func NewServerPool(reg *backend.Registry) (*ServerPool, error) {
	return &ServerPool{reg: reg}, nil
}

// GetServer selects the next available URL using Round Robin.
func (s *ServerPool) GetServer() (*url.URL, error) {
	backends := s.reg.ListBackends()
	if len(backends) == 0 {
		return nil, ErrNoBackends
	}
	next := s.current.Add(1)
	// TODO: Rethink if we want .URL or no
	return backends[(next-1)%uint64(len(backends))].URL, nil
}
