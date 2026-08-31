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
	// Balancer chooses the backend for a request.
	Balancer interface {
		Select(r *http.Request) (*domain.Backend, error)
	}

	BalancerFunc func(r *http.Request) (*domain.Backend, error)

	// RotationBalancer picks from internal state only (round-robin, least-conn, random).
	RotationBalancer interface {
		GetServer() (*domain.Backend, error)
	}

	// AffinityBalancer picks from a key, for sticky routing (e.g. hash).
	AffinityBalancer interface {
		GetServerForKey(key string) (*domain.Backend, error)
	}

	Handler struct {
		balancer   Balancer
		MaxRetries int
		transport  http.RoundTripper // nil falls back to http.DefaultTransport; tests inject a fake
	}
)

func (f BalancerFunc) Select(r *http.Request) (*domain.Backend, error) { return f(r) }

// Rotate adapts a RotationBalancer into a Balancer, ignoring the request.
func Rotate(b RotationBalancer) Balancer {
	return BalancerFunc(func(*http.Request) (*domain.Backend, error) { return b.GetServer() })
}

// ByClientIP adapts an AffinityBalancer into a Balancer keyed on caller IP.
func ByClientIP(b AffinityBalancer) Balancer {
	return BalancerFunc(func(r *http.Request) (*domain.Backend, error) {
		return b.GetServerForKey(clientIP(r))
	})
}

func NewHandler(b Balancer, maxRetries int) *Handler {
	return &Handler{balancer: b, MaxRetries: maxRetries}
}

// Routes mounts the proxy as the catch-all handler for any request.
func (h *Handler) Routes(mux *http.ServeMux) {
	mux.Handle("/", h)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Buffer the body up front so each retry attempt can rewind it — ReverseProxy
	// consumes and closes r.Body on every attempt.
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

	// Try the initial attempt plus up to MaxRetries failovers. ReverseProxy
	// reports unreachable backends via ErrorHandler before any response byte
	// reaches the client, so retrying on the same ResponseWriter is safe.
	//
	// Note: sticky balancers select deterministically, so a reachable-but-failing
	// backend gets re-selected each attempt until the prober evicts it.
	for attempt := 0; attempt <= h.MaxRetries; attempt++ {
		if r.GetBody != nil {
			body, err := r.GetBody()
			if err != nil {
				http.Error(w, "failed to rewind request body", http.StatusInternalServerError)
				return
			}
			r.Body = body
		}

		target, err := h.balancer.Select(r)
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

// proxyOnce reverse-proxies r to target once. Returns true if the client
// response was written (request done), false if target was unreachable and
// the caller should fail over. The connection count is held only for this
// attempt, so a failed backend isn't left counted while another one serves.
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
			served = false // signal a retry instead of writing an error here
		},
	}

	rp.ServeHTTP(w, r)
	return served
}

// clientIP returns the caller's IP, stripping the port from RemoteAddr.
// X-Forwarded-For is ignored deliberately: it's client-controlled, so
// trusting it would let a caller steer its own IP-hash routing.
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
