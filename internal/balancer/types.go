package balancer

import (
	"errors"

	"github.com/treska03/golancer/internal/domain"
)

var ErrNoBackends = errors.New("no backend servers available")

// ServerPool manages a list of backends.
type (
	Registry interface {
		ListBackends() []*domain.Backend
	}

	HealthyRegistry interface {
		ListHealthyBackends() []*domain.Backend
	}
)
