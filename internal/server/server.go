package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/treska03/golancer/internal/backend"
	"github.com/treska03/golancer/internal/handlers"
	"github.com/treska03/golancer/internal/proxy"
)

type Config struct {
	Port         int
	MaxRetries   int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// internal/server/server.go
func New(reg *backend.Registry, pool proxy.Balancer, cfg *Config) *http.Server {
	lb := proxy.NewHandler(pool, cfg.MaxRetries)
	backends := handlers.NewBackendHandler(reg)
	health := handlers.NewHealthHandler(reg)

	return &http.Server{
		Addr:         ":" + strconv.Itoa(cfg.Port),
		Handler:      newRouter(backends, health, lb),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
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
