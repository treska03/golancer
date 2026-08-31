package domain

import (
	"encoding/json"
	"net/url"
	"sync/atomic"

	"github.com/google/uuid"
)

type Backend struct {
	InstanceID          string
	URL                 *url.URL
	healthy             atomic.Bool
	ActiveConnectionsNo atomic.Int32
	Weight              uint64
}

// NewBackend creates a Backend for u with a freshly generated instance ID.
func NewBackend(u *url.URL, w uint64) *Backend {
	return &Backend{InstanceID: uuid.New().String(), URL: u, Weight: w}
}

func (b *Backend) Healthy() bool { return b.healthy.Load() }

func (b *Backend) SetHealthy(v bool) { b.healthy.Store(v) }

// MarshalJSON renders a Backend with its URL as a plain string. url.URL has no
// JSON representation of its own and would otherwise be encoded as a verbose
// nested struct (Scheme, Host, Path, ...), which is not usable by API clients.
func (b *Backend) MarshalJSON() ([]byte, error) {
	var u string
	if b.URL != nil {
		u = b.URL.String()
	}
	return json.Marshal(struct {
		InstanceID string `json:"instanceID"`
		URL        string `json:"url"`
		Healthy    bool   `json:"healthy"`
	}{
		InstanceID: b.InstanceID,
		URL:        u,
		Healthy:    b.healthy.Load(),
	})
}
