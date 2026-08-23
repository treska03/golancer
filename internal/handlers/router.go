package handlers

import "net/http"

// RouteRegistrar is anything that can mount its own routes onto a mux.
type RouteRegistrar interface {
	Routes(*http.ServeMux)
}

// NewRouter composes the given registrars onto a single mux
func NewRouter(registrars ...RouteRegistrar) http.Handler {
	mux := http.NewServeMux()
	for _, r := range registrars {
		r.Routes(mux)
	}
	return mux
}
