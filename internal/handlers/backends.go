package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/treska03/golancer/internal/domain"
)

type RegisterBackendRequest struct {
	URL    string `json:"url"`
	Weight uint64 `json:"weight"`
}

// BackendHandler serves the backend management API.
type BackendHandler struct {
	reg Registry
}

type Registry interface {
	AddNewBackend(*url.URL, uint64) (string, error)
	ListBackends() []*domain.Backend
	RemoveInstanceByID(string) bool
}

func NewBackendHandler(reg Registry) *BackendHandler {
	return &BackendHandler{reg: reg}
}

// Routes registers the backend management endpoints on the given mux.
func (h *BackendHandler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /backends", h.list)
	mux.HandleFunc("POST /backends", h.register)
	mux.HandleFunc("DELETE /backends/{instanceID}", h.deregister)
}

func (h *BackendHandler) list(w http.ResponseWriter, r *http.Request) {
	backends := h.reg.ListBackends()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string][]*domain.Backend{"backends": backends})
}

func (h *BackendHandler) register(w http.ResponseWriter, r *http.Request) {
	u, weight, err := parseRegisterBackendReq(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, err := h.reg.AddNewBackend(u, weight)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"instanceID": id})
}

func (h *BackendHandler) deregister(w http.ResponseWriter, r *http.Request) {
	target := r.PathValue("instanceID")
	if target == "" {
		http.Error(w, "instanceID must be provided", http.StatusBadRequest)
		return
	}
	if ok := h.reg.RemoveInstanceByID(target); !ok {
		http.Error(w, fmt.Sprintf("backend with instanceID %s not found", target), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func parseRegisterBackendReq(r *http.Request) (*url.URL, uint64, error) {
	defer r.Body.Close()

	var req RegisterBackendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, 0, err
	}

	// Parse the string into a *url.URL struct. url.Parse accepts relative
	// references (e.g. "foo") without error, so we must additionally require an
	// absolute URL with both a scheme and a host.
	parsedURL, err := url.Parse(req.URL)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid url %q: %w", req.URL, err)
	}
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return nil, 0, fmt.Errorf("url %q must include a scheme and host", req.URL)
	}

	// A missing/zero weight defaults to 1, matching config-loaded backends.
	weight := req.Weight
	if weight == 0 {
		weight = 1
	}

	return parsedURL, weight, nil
}
