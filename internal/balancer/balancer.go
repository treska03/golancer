package balancer

import (
	"errors"
	"golancer/m/internal/servers"
	"net/url"
	"sync/atomic"
)

var ErrNoBackends = errors.New("no backend servers available")

// ServerPool manages a list of backend target URLs.
type ServerPool struct {
	backends []*url.URL
	current  atomic.Uint64
}

func NewServerPool() (*ServerPool, error) {
	backends, err := servers.ListURL()
	if err != nil {
		return nil, err
	}
	return &ServerPool{backends: backends}, nil
}

// GetServer selects the next available URL using Round Robin.
func (s *ServerPool) GetServer() (*url.URL, error) {
	if len(s.backends) == 0 {
		return nil, ErrNoBackends
	}
	next := s.current.Add(1)
	return s.backends[(next-1)%uint64(len(s.backends))], nil
}