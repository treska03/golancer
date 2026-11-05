package proxy

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"slices"
	"strings"

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
		trusted    TrustedProxies
		transport  http.RoundTripper // nil falls back to http.DefaultTransport; tests inject a fake
	}

	// TrustedProxies lists the networks of reverse proxies in front of the
	// balancer (ingress controllers, cloud LBs). Forwarding headers are only
	// believed when the immediate peer is inside one of them. Empty means no
	// proxy is trusted and only RemoteAddr counts.
	TrustedProxies []netip.Prefix
)

func (f BalancerFunc) Select(r *http.Request) (*domain.Backend, error) { return f(r) }

// Rotate adapts a RotationBalancer into a Balancer, ignoring the request.
func Rotate(b RotationBalancer) Balancer {
	return BalancerFunc(func(*http.Request) (*domain.Backend, error) { return b.GetServer() })
}

// ByClientIP adapts an AffinityBalancer into a Balancer keyed on caller IP,
// resolved through trusted (see TrustedProxies.ClientIP).
func ByClientIP(b AffinityBalancer, trusted TrustedProxies) Balancer {
	return BalancerFunc(func(r *http.Request) (*domain.Backend, error) {
		return b.GetServerForKey(trusted.ClientIP(r))
	})
}

func NewHandler(b Balancer, maxRetries int, trusted TrustedProxies) *Handler {
	return &Handler{balancer: b, MaxRetries: maxRetries, trusted: trusted}
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
	for attempt := 0; attempt <= h.maxRetriesFor(r); attempt++ {
		if r.Context().Err() != nil {
			return
		}

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

// maxRetriesFor returns how many failovers r may get. POST and PATCH may not be
// idempotent, so we do not retry these.
func (h *Handler) maxRetriesFor(r *http.Request) int {
	switch r.Method {
	case http.MethodPost, http.MethodPatch:
		return 0
	}
	return h.MaxRetries
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
			// SetURL joins the backend's base path with the request path
			// (http://svc/api + /users -> /api/users), so traffic goes where
			// the prober checks health, and sets Host to the backend.
			req.SetURL(target.URL)
			h.setForwarded(req)
		},
		ErrorHandler: func(http.ResponseWriter, *http.Request, error) {
			served = false // signal a retry instead of writing an error here
		},
	}

	rp.ServeHTTP(w, r)
	return served
}

// setForwarded sets X-Forwarded-For/-Host/-Proto on the outbound request.
// Rewrite strips the inbound copies, which is right for direct clients (they
// could forge them). When the peer is a trusted proxy, its X-Forwarded-For
// chain and X-Forwarded-Proto are kept so backends see the real client and
// scheme (e.g. https terminated at the ingress).
func (h *Handler) setForwarded(req *httputil.ProxyRequest) {
	fromTrusted := h.trusted.contains(peerIP(req.In))
	if fromTrusted {
		if xff := req.In.Header.Values("X-Forwarded-For"); len(xff) > 0 {
			req.Out.Header["X-Forwarded-For"] = xff
		}
	}
	req.SetXForwarded() // appends the peer to X-Forwarded-For; sets Host and Proto
	if proto := req.In.Header.Get("X-Forwarded-Proto"); fromTrusted && proto != "" {
		req.Out.Header.Set("X-Forwarded-Proto", proto)
	}
}

// ClientIP returns the originating client's IP for r. If the peer is not a
// trusted proxy, that's simply the peer: X-Forwarded-For is client-controlled
// and trusting it would let a caller steer its own IP-hash routing. If the
// peer is trusted, X-Forwarded-For is walked right to left (each proxy appends
// the address it saw) and the first address outside the trusted set wins.
func (t TrustedProxies) ClientIP(r *http.Request) string {
	peer := peerIP(r)
	if !t.contains(peer) {
		return hostOnly(r.RemoteAddr)
	}

	var hops []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(v, ",")...)
	}
	client := peer
	for _, hop := range slices.Backward(hops) {
		addr, err := netip.ParseAddr(strings.TrimSpace(hop))
		if err != nil {
			break // malformed hop: don't trust anything to its left
		}
		client = addr.Unmap()
		if !t.contains(client) {
			break
		}
	}
	return client.String()
}

func (t TrustedProxies) contains(addr netip.Addr) bool {
	if !addr.IsValid() {
		return false
	}
	for _, p := range t {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// peerIP returns the immediate peer's address, or the zero Addr if RemoteAddr
// doesn't parse.
func peerIP(r *http.Request) netip.Addr {
	addr, err := netip.ParseAddr(hostOnly(r.RemoteAddr))
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}

// hostOnly strips the port from a host:port address.
func hostOnly(hostport string) string {
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return host
	}
	return hostport
}
