package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/treska03/golancer/internal/domain"
)

// ReadinessChecker reports the balancer's currently routable backends. The
// balancer is ready to serve proxied traffic only while at least one backend is
// healthy; with none, every proxied request fails with 503, so readiness probes
// should see the instance as unavailable.
type ReadinessChecker interface {
	ListHealthyBackends() []*domain.Backend
}

// HealthHandler serves the balancer's own liveness (/healthz) and readiness
// (/readyz) endpoints. These describe the load-balancer process itself and are
// distinct from the per-backend health tracked by the registry and probed on
// each upstream.
type HealthHandler struct {
	ready ReadinessChecker
}

// NewHealthHandler builds a HealthHandler whose readiness reflects ready.
func NewHealthHandler(ready ReadinessChecker) *HealthHandler {
	return &HealthHandler{ready: ready}
}

// Routes registers the liveness and readiness endpoints on the given mux.
func (h *HealthHandler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /readyz", h.readyz)
}

// healthz is a liveness probe: it reports that the process is up and its HTTP
// server is serving. It deliberately does not inspect backends — a running
// balancer with zero healthy backends is still alive and should be kept out of
// rotation (see readyz), not restarted.
func (h *HealthHandler) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// readyz is a readiness probe: it reports 200 only while at least one backend
// is healthy and therefore routable, and 503 otherwise so an upstream load
// balancer stops sending traffic to a balancer that can serve none.
func (h *HealthHandler) readyz(w http.ResponseWriter, _ *http.Request) {
	healthy := len(h.ready.ListHealthyBackends())

	status, label := http.StatusOK, "ready"
	if healthy == 0 {
		status, label = http.StatusServiceUnavailable, "unavailable"
	}
	writeJSON(w, status, map[string]any{
		"status":          label,
		"healthyBackends": healthy,
	})
}

// writeJSON writes v as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
