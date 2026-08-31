package proxy

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httputil"

	"github.com/treska03/golancer/internal/domain"
)

type (
	// Selector resolves the backend an incoming request should be forwarded to.
	// The proxy hands over the whole request; each Selector pulls out only what
	// its strategy needs (a client IP, a header, or nothing) and delegates the
	// actual choice to a balancing strategy. Build one from a strategy with the
	// Stateless / ByClientIP adapters, or with SelectorFunc for a custom key.
	Selector interface {
		Select(r *http.Request) (*domain.Backend, error)
	}

	// SelectorFunc adapts an ordinary function to a Selector.
	SelectorFunc func(r *http.Request) (*domain.Backend, error)

	// SimpleSelector is a strategy whose choice does not depend on the request
	// at all (round-robin, least-connections, random).
	SimpleSelector interface {
		GetServer() (*domain.Backend, error)
	}

	// KeySelector is a strategy that routes on a single opaque string key, such
	// as hash routing. It never sees the request — the adapter extracts the key
	// and hands over only that — so the same strategy serves IP-hash,
	// country-hash, header-hash, etc.
	KeySelector interface {
		GetServerForKey(key string) (*domain.Backend, error)
	}

	// Handler asks the Selector for a backend and reverse-proxies the request to it.
	Handler struct {
		selector   Selector
		MaxRetries int
		// transport is the RoundTripper used for upstream requests. When nil the
		// ReverseProxy falls back to http.DefaultTransport; tests inject a fake.
		transport http.RoundTripper
	}
)

func (f SelectorFunc) Select(r *http.Request) (*domain.Backend, error) { return f(r) }

// Stateless adapts a request-agnostic strategy (round-robin, least-connections,
// random) into a Selector, discarding the request.
func Stateless(s SimpleSelector) Selector {
	return SelectorFunc(func(*http.Request) (*domain.Backend, error) { return s.GetServer() })
}

// ByClientIP adapts a key-based strategy into a Selector that routes on the
// caller's IP address, giving the strategy only that string. Pairing a
// HashSelector with ByClientIP yields IP-hash routing; pairing the same
// HashSelector with a future ByClientCountry adapter would yield country-hash,
// with no change to the strategy.
func ByClientIP(s KeySelector) Selector {
	return SelectorFunc(func(r *http.Request) (*domain.Backend, error) {
		return s.GetServerForKey(clientIP(r))
	})
}

func NewHandler(s Selector, maxRetries int) *Handler {
	return &Handler{selector: s, MaxRetries: maxRetries}
}

// Routes mounts the proxy as the catch-all handler for any request.
func (h *Handler) Routes(mux *http.ServeMux) {
	mux.Handle("/", h)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// ReverseProxy consumes and closes r.Body on every attempt, so buffer it up
	// front and expose a GetBody that hands out a fresh reader for each retry.
	// Without this, a request with a body that fails over would reach the next
	// backend with an empty body.
	if r.Body != nil {
		body, err := io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}
		r.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		}
	}

	// Try the initial attempt plus up to MaxRetries failovers. Each iteration
	// selects a backend and reverse-proxies to it; a backend that is unreachable
	// is retried against the next backend. ReverseProxy reports such failures via
	// its ErrorHandler before any response byte reaches the client, so retrying
	// on the same ResponseWriter is safe.
	//
	// Note: sticky strategies (hash routing) select deterministically from the
	// request, so if their chosen backend is reachable-but-failing they will
	// re-select it each attempt; failover only helps once the prober evicts it
	// from the healthy set.
	for attempt := 0; attempt <= h.MaxRetries; attempt++ {
		// Rewind the body for this attempt; the previous attempt drained it.
		if r.GetBody != nil {
			body, err := r.GetBody()
			if err != nil {
				http.Error(w, "failed to rewind request body", http.StatusInternalServerError)
				return
			}
			r.Body = body
		}

		// Ask the selector where to send this request.
		target, err := h.selector.Select(r)
		if err != nil {
			http.Error(w, "no healthy backends registered", http.StatusServiceUnavailable)
			return
		}

		if h.proxyOnce(w, r, target) {
			return // request was served (success or a non-retryable response)
		}
	}

	http.Error(w, "no healthy backend could serve the request", http.StatusServiceUnavailable)
}

// proxyOnce reverse-proxies r to target exactly once. It reports true when the
// client response was written (the request is done) and false when target was
// unreachable and the caller should fail over to another backend. The target's
// in-flight connection count is held only for the duration of this attempt, so
// a backend that fails is not left counted while a later backend serves.
func (h *Handler) proxyOnce(w http.ResponseWriter, r *http.Request, target *domain.Backend) bool {
	target.ActiveConnectionsNo.Add(1)
	defer target.ActiveConnectionsNo.Add(-1)

	served := true
	rp := &httputil.ReverseProxy{
		Transport: h.transport,
		Rewrite: func(req *httputil.ProxyRequest) {
			req.Out.URL.Scheme = target.URL.Scheme
			req.Out.URL.Host = target.URL.Host
			req.Out.URL.Path = req.In.URL.Path
			req.Out.Header.Set("X-Forwarded-Host", req.In.Host)
			req.Out.Host = target.URL.Host
		},
		ErrorHandler: func(http.ResponseWriter, *http.Request, error) {
			// Transport-level failure before anything was written to the client.
			// Signal a retry instead of writing an error response here.
			served = false
		},
	}

	rp.ServeHTTP(w, r)
	return served
}

// clientIP returns the caller's IP address, stripping the port from RemoteAddr.
// X-Forwarded-For is deliberately ignored: it is client-controlled, so trusting
// it would let a caller steer its own IP-hash routing.
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
