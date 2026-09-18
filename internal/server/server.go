package server

import (
	"net/http"
	"strconv"
	"time"
)

type Config struct {
	Port         int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

type Middleware func(next http.Handler) http.Handler

func New(cfg *Config, middleware Middleware, registrars ...RouteRegistrar) *http.Server {
	h := newRouter(registrars...)
	if middleware != nil {
		h = middleware(h)
	}
	return &http.Server{
		Addr:         ":" + strconv.Itoa(cfg.Port),
		Handler:      h,
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
