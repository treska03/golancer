package proxy

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/treska03/golancer/internal/domain"
)

// seqSelector returns the given backends in order, one per call.
type seqSelector struct {
	backends []*domain.Backend
	i        atomic.Int64
}

func (s *seqSelector) Select(*http.Request) (*domain.Backend, error) {
	idx := s.i.Add(1) - 1
	return s.backends[idx%int64(len(s.backends))], nil
}

func newSeqSelector(rawURLs ...string) *seqSelector {
	backends := make([]*domain.Backend, 0, len(rawURLs))
	for _, raw := range rawURLs {
		u, _ := url.Parse(raw)
		backends = append(backends, domain.NewBackend(u, 1))
	}
	return &seqSelector{backends: backends}
}

// keyFunc is a KeySelector that records the key it was given.
type keyFunc func(string) (*domain.Backend, error)

func (f keyFunc) GetServerForKey(key string) (*domain.Backend, error) { return f(key) }

// TestByClientIPExtractsHost verifies the ByClientIP adapter hands the strategy
// only the client's IP (host portion of RemoteAddr, no port).
func TestByClientIPExtractsHost(t *testing.T) {
	var gotKey string
	ks := keyFunc(func(key string) (*domain.Backend, error) {
		gotKey = key
		u, _ := url.Parse("http://x.invalid")
		return domain.NewBackend(u, 1), nil
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.9:54321"
	if _, err := ByClientIP(ks).Select(req); err != nil {
		t.Fatalf("Select: %v", err)
	}
	if gotKey != "203.0.113.9" {
		t.Fatalf("expected key 203.0.113.9, got %q", gotKey)
	}
}

// fakeTransport fails the first N round trips (as if the backend were dead),
// then serves a 200 while recording the body it received.
type fakeTransport struct {
	failFirst int
	calls     int
	gotBody   string
}

func (t *fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls++
	if t.calls <= t.failFirst {
		return nil, errors.New("connection refused")
	}
	b, _ := io.ReadAll(req.Body)
	t.gotBody = string(b)
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("ok")),
		Header:     make(http.Header),
	}, nil
}

// TestFailoverReplaysBody verifies that when the first backend fails at
// connection time, the request body is replayed to the next backend.
func TestFailoverReplaysBody(t *testing.T) {
	ft := &fakeTransport{failFirst: 1}
	h := NewHandler(newSeqSelector("http://backend-1.invalid", "http://backend-2.invalid"), 3)
	h.transport = ft

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello-body"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after failover, got %d", rec.Code)
	}
	if ft.calls != 2 {
		t.Fatalf("expected 2 upstream attempts, got %d", ft.calls)
	}
	if ft.gotBody != "hello-body" {
		t.Fatalf("expected replayed body %q, got %q", "hello-body", ft.gotBody)
	}
}

// TestRetriesExhausted verifies a 503 once every attempt fails.
func TestRetriesExhausted(t *testing.T) {
	ft := &fakeTransport{failFirst: 99}
	h := NewHandler(newSeqSelector("http://backend-1.invalid"), 2)
	h.transport = ft

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when retries exhausted, got %d", rec.Code)
	}
	// MaxRetries=2 means attempts at retryCount 0,1,2 => 3 upstream calls.
	if ft.calls != 3 {
		t.Fatalf("expected 3 attempts (initial + 2 retries), got %d", ft.calls)
	}
}
