package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/treska03/golancer/internal/domain"
)

// stubReadiness is a ReadinessChecker returning a fixed set of healthy backends.
type stubReadiness struct {
	healthy []*domain.Backend
}

func (s stubReadiness) ListHealthyBackends() []*domain.Backend { return s.healthy }

func newBackends(n int) []*domain.Backend {
	out := make([]*domain.Backend, n)
	for i := range out {
		u, _ := url.Parse("http://127.0.0.1")
		out[i] = domain.NewBackend(u, 1)
	}
	return out
}

// serve routes req through a HealthHandler backed by ready and returns the
// recorded response.
func serve(ready ReadinessChecker, method, target string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	NewHealthHandler(ready).Routes(mux)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(method, target, nil))
	return rr
}

func TestHealthzAlwaysOK(t *testing.T) {
	// Liveness must not depend on backend health: even with zero healthy
	// backends the process is alive and should report ok.
	for _, n := range []int{0, 3} {
		rr := serve(stubReadiness{newBackends(n)}, http.MethodGet, "/healthz")

		if rr.Code != http.StatusOK {
			t.Fatalf("healthz with %d healthy backends: status = %d, want %d", n, rr.Code, http.StatusOK)
		}
		var body struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatalf("decoding body: %v", err)
		}
		if body.Status != "ok" {
			t.Fatalf("status = %q, want %q", body.Status, "ok")
		}
	}
}

func TestReadyzReadyWithHealthyBackends(t *testing.T) {
	rr := serve(stubReadiness{newBackends(2)}, http.MethodGet, "/readyz")

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var body struct {
		Status          string `json:"status"`
		HealthyBackends int    `json:"healthyBackends"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body.Status != "ready" {
		t.Fatalf("status = %q, want %q", body.Status, "ready")
	}
	if body.HealthyBackends != 2 {
		t.Fatalf("healthyBackends = %d, want 2", body.HealthyBackends)
	}
}

func TestReadyzUnavailableWithNoHealthyBackends(t *testing.T) {
	rr := serve(stubReadiness{nil}, http.MethodGet, "/readyz")

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
	var body struct {
		Status          string `json:"status"`
		HealthyBackends int    `json:"healthyBackends"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body.Status != "unavailable" {
		t.Fatalf("status = %q, want %q", body.Status, "unavailable")
	}
	if body.HealthyBackends != 0 {
		t.Fatalf("healthyBackends = %d, want 0", body.HealthyBackends)
	}
}
