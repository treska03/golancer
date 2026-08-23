package domain

import (
	"encoding/json"
	"net/url"

	"github.com/google/uuid"
)

type Backend struct {
	InstanceID string
	URL        *url.URL
}

// NewBackend creates a Backend for u with a freshly generated instance ID.
func NewBackend(u *url.URL) *Backend {
	return &Backend{InstanceID: uuid.New().String(), URL: u}
}

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
	}{
		InstanceID: b.InstanceID,
		URL:        u,
	})
}
