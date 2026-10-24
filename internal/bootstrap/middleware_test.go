package bootstrap

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/treska03/golancer/internal/server"
)

// TestRouteLabelUsesPattern: distinct paths hitting the same catch-all route
// share one label, so the metric's cardinality is bounded by the route set.
func TestRouteLabelUsesPattern(t *testing.T) {
	var got []string
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("DELETE /backends/{instanceID}", func(http.ResponseWriter, *http.Request) {})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r)
		got = append(got, routeLabel(r))
	})

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/a1", nil),
		httptest.NewRequest(http.MethodGet, "/a2/b?x=1", nil),
		httptest.NewRequest(http.MethodDelete, "/backends/123", nil),
		httptest.NewRequest(http.MethodDelete, "/backends/456", nil),
	} {
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	want := []string{"/", "/", "DELETE /backends/{instanceID}", "DELETE /backends/{instanceID}"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("request %d: label = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBearerAuth(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	cases := []struct {
		name, token, auth string
		want              int
	}{
		{"no token configured", "", "", http.StatusNoContent},
		{"missing header", "s3cret", "", http.StatusUnauthorized},
		{"wrong token", "s3cret", "Bearer nope", http.StatusUnauthorized},
		{"correct token", "s3cret", "Bearer s3cret", http.StatusNoContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/backends", nil)
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			rr := httptest.NewRecorder()
			bearerAuth(tc.token)(ok).ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("status = %d, want %d", rr.Code, tc.want)
			}
		})
	}
}

// TestChainOrder: the first middleware passed to chain is the outermost.
func TestChainOrder(t *testing.T) {
	var order []string
	mark := func(name string) server.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}
	h := chain(mark("outer"), mark("inner"))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		order = append(order, "handler")
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if got := strings.Join(order, ","); got != "outer,inner,handler" {
		t.Fatalf("order = %s, want outer,inner,handler", got)
	}
}
