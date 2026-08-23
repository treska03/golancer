package proxy

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

type (
	// Selector picks the backend a request should be forwarded to.
	Selector interface {
		GetServer() (*url.URL, error)
	}

	// Handler asks the Selector for a backend and reverse-proxies the request to it.
	Handler struct {
		selector Selector
	}
)

func NewHandler(s Selector) *Handler {
	return &Handler{selector: s}
}

// Routes mounts the proxy as the catch-all handler for any request.
func (h *Handler) Routes(mux *http.ServeMux) {
	mux.Handle("/", h)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Ask the selector where to send this request.
	target, err := h.selector.GetServer()
	if err != nil {
		http.Error(w, "no healthy backends registered", http.StatusServiceUnavailable)
		return
	}

	rp := &httputil.ReverseProxy{
		Rewrite: func(req *httputil.ProxyRequest) {
			req.Out.URL.Scheme = target.Scheme
			req.Out.URL.Host = target.Host
			req.Out.URL.Path = req.In.URL.Path
			req.Out.Header.Set("X-Forwarded-Host", req.In.Host)
			req.Out.Host = target.Host
		},
	}

	rp.ServeHTTP(w, r)
}
