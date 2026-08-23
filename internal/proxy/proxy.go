package proxy

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// Balancer defines an interface for getting the target server.
type Balancer interface {
	GetServer() (*url.URL, error)
}

// ProxyHandler implements http.Handler to forward traffic.
type ProxyHandler struct {
	balancer Balancer
}

func NewProxyHandler(b Balancer) *ProxyHandler {
	return &ProxyHandler{balancer: b}
}

func (p *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Ask the balancer where to send this request
	target, err := p.balancer.GetServer()
	if err != nil {
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}

	// Single-request reverse proxy instance configured for the selected target
	proxy := &httputil.ReverseProxy{
		Rewrite: func(req *httputil.ProxyRequest) {
			req.Out.URL.Scheme = target.Scheme
			req.Out.URL.Host = target.Host
			req.Out.URL.Path = r.URL.Path
			req.Out.Header.Set("X-Forwarded-Host", r.Host)
			req.Out.Host = target.Host
		},
	}

	proxy.ServeHTTP(w, r)
}
