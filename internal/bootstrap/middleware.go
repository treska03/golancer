package bootstrap

import (
	"bufio"
	"net"
	"net/http"

	"github.com/treska03/golancer/internal/metrics"
	"github.com/treska03/golancer/internal/server"
)

func commonMiddleware(m *metrics.Common) server.Middleware {
	return requestMetrics(m)
}

// requestMetrics returns middleware that reports each request to m. The HTTP
// concerns (capturing the status, reading request fields) live here so the
// metrics package stays independent of the HTTP layer.
func requestMetrics(m *metrics.Common) server.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			m.ObserveRequest(r.URL.EscapedPath(), r.Method, rec.status)
		})
	}
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
