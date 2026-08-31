package server

import (
	"net/http"
	"time"

	"github.com/treska03/golancer/internal/backend"
	"github.com/treska03/golancer/internal/handlers"
	"github.com/treska03/golancer/internal/proxy"
)

// internal/server/server.go
func New(reg *backend.Registry, pool proxy.Selector, mr int) *http.Server {
	lb := proxy.NewHandler(pool, mr)
	backends := handlers.NewBackendHandler(reg)

	return &http.Server{
		Addr:         ":8080",
		Handler:      newRouter(backends, lb),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
}

// RouteRegistrar is anything that can mount its own routes onto a mux.
type RouteRegistrar interface {
	Routes(*http.ServeMux)
}

// NewRouter composes the given registrars onto a single mux
func newRouter(registrars ...RouteRegistrar) http.Handler {
	mux := http.NewServeMux()
	for _, r := range registrars {
		r.Routes(mux)
	}
	return mux
}
