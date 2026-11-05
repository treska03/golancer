package bootstrap

import (
	"bufio"
	"crypto/subtle"
	"net"
	"net/http"
	"slices"

	"github.com/treska03/golancer/internal/metrics"
	"github.com/treska03/golancer/internal/server"
)

func commonMiddleware(m *metrics.Common, bodyLimit int64) server.Middleware {
	return chain(requestMetrics(m), limitBody(bodyLimit))
}

// chain composes middlewares so the first one listed is the outermost.
func chain(mws ...server.Middleware) server.Middleware {
	return func(next http.Handler) http.Handler {
		for _, mw := range slices.Backward(mws) {
			next = mw(next)
		}
		return next
	}
}

// bearerAuth returns middleware that rejects requests lacking
// "Authorization: Bearer <token>". An empty token disables the check. The
// comparison is constant-time so the token can't be recovered by timing.
func bearerAuth(token string) server.Middleware {
	return func(next http.Handler) http.Handler {
		if token == "" {
			return next
		}
		want := []byte("Bearer " + token)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func limitBody(limit int64) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}

// requestMetrics returns middleware that reports each request to m. The HTTP
// concerns (capturing the status, reading request fields) live here so the
// metrics package stays independent of the HTTP layer.
func requestMetrics(m *metrics.Common) server.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			m.ObserveRequest(routeLabel(r), r.Method, rec.status)
		})
	}
}

// routeLabel returns the matched ServeMux pattern (e.g. "/" or
// "DELETE /backends/{instanceID}") rather than the raw request path. The proxy
// is a catch-all, so labelling by raw path would mint a new time series for
// every distinct URL a client sends. ServeMux sets r.Pattern in place during
// routing, so it is populated once next has run.
func routeLabel(r *http.Request) string {
	if r.Pattern == "" {
		return "unmatched"
	}
	return r.Pattern
}

// statusRecorder wraps http.ResponseWriter to capture the response status code.
// It also forwards the optional Flusher and Hijacker interfaces so the reverse
// proxy can still stream responses and upgrade connections through it.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	// A write with no prior WriteHeader implies the default 200, already set.
	r.wroteHeader = true
	return r.ResponseWriter.Write(b)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := r.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}
