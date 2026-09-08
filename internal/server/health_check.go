package server

import "net/http"

type Handler struct {
}

func NewHandler() *Handler {
	return &Handler{}
}

// Routes mounts the proxy as the catch-all handler for any request.
func (h *Handler) Routes(mux *http.ServeMux) {
	mux.Handle("/", h)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {

}
