package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/treska03/golancer/internal/domain"
)

// fakeRegistry records added backends and accepts every registration.
type fakeRegistry struct {
	added []*domain.Backend
}

func (f *fakeRegistry) AddNewBackend(u *url.URL, w uint64) (string, error) {
	b := domain.NewBackend(u, w)
	f.added = append(f.added, b)
	return b.InstanceID, nil
}
func (f *fakeRegistry) ListBackends() []*domain.Backend { return f.added }
func (f *fakeRegistry) RemoveInstanceByID(string) bool  { return true }

func serveBackends(method, target, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	NewBackendHandler(&fakeRegistry{}).Routes(mux)

	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func TestRegisterRejectsInvalidInput(t *testing.T) {
	cases := map[string]string{
		"weight over max": `{"url":"http://a.invalid","weight":9223372036854775808}`,
		"non-http scheme": `{"url":"ftp://a.invalid","weight":1}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rr := serveBackends(http.MethodPost, "/backends", body)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
			}
		})
	}
}
