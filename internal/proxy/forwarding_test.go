package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

// recordingTransport serves a 200 and keeps the last outbound request.
type recordingTransport struct {
	got *http.Request
}

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.got = req
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("ok")),
		Header:     make(http.Header),
	}, nil
}

func trusted(cidrs ...string) TrustedProxies {
	out := make(TrustedProxies, 0, len(cidrs))
	for _, c := range cidrs {
		out = append(out, netip.MustParsePrefix(c))
	}
	return out
}

// proxyThrough sends req through a Handler pointed at backendURL and returns
// the request the backend would have received.
func proxyThrough(t *testing.T, backendURL string, tp TrustedProxies, req *http.Request) *http.Request {
	t.Helper()
	rt := &recordingTransport{}
	h := NewHandler(newSeqSelector(backendURL), 0, tp)
	h.transport = rt
	h.ServeHTTP(httptest.NewRecorder(), req)
	if rt.got == nil {
		t.Fatal("request never reached the backend")
	}
	return rt.got
}

// TestBackendBasePathJoined: a backend registered with a path prefix receives
// requests under that prefix, matching where the prober checks its health.
func TestBackendBasePathJoined(t *testing.T) {
	cases := map[string]struct{ backend, in, wantPath, wantQuery string }{
		"no base path":       {"http://svc.invalid", "/users?id=1", "/users", "id=1"},
		"base path":          {"http://svc.invalid/api", "/users?id=1", "/api/users", "id=1"},
		"trailing slash":     {"http://svc.invalid/api/", "/users", "/api/users", ""},
		"backend query kept": {"http://svc.invalid/api?v=2", "/users?id=1", "/api/users", "v=2&id=1"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			out := proxyThrough(t, tc.backend, nil, httptest.NewRequest(http.MethodGet, tc.in, nil))
			if out.URL.Path != tc.wantPath || out.URL.RawQuery != tc.wantQuery {
				t.Fatalf("outbound = %s?%s, want %s?%s", out.URL.Path, out.URL.RawQuery, tc.wantPath, tc.wantQuery)
			}
			if out.URL.Host != "svc.invalid" {
				t.Fatalf("outbound host = %q, want svc.invalid", out.URL.Host)
			}
		})
	}
}

func TestForwardedHeaders(t *testing.T) {
	newReq := func(remote string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "http://lb.example/", nil)
		r.RemoteAddr = remote
		r.Header.Set("X-Forwarded-For", "198.51.100.7")
		r.Header.Set("X-Forwarded-Proto", "https")
		return r
	}

	t.Run("untrusted peer: forged headers dropped", func(t *testing.T) {
		out := proxyThrough(t, "http://svc.invalid", trusted("10.0.0.0/8"), newReq("203.0.113.9:5000"))
		if got := out.Header.Get("X-Forwarded-For"); got != "203.0.113.9" {
			t.Errorf("X-Forwarded-For = %q, want only the peer 203.0.113.9", got)
		}
		if got := out.Header.Get("X-Forwarded-Proto"); got != "http" {
			t.Errorf("X-Forwarded-Proto = %q, want http", got)
		}
		if got := out.Header.Get("X-Forwarded-Host"); got != "lb.example" {
			t.Errorf("X-Forwarded-Host = %q, want lb.example", got)
		}
	})

	t.Run("trusted peer: chain and proto kept", func(t *testing.T) {
		out := proxyThrough(t, "http://svc.invalid", trusted("10.0.0.0/8"), newReq("10.1.2.3:5000"))
		if got := out.Header.Get("X-Forwarded-For"); got != "198.51.100.7, 10.1.2.3" {
			t.Errorf("X-Forwarded-For = %q, want %q", got, "198.51.100.7, 10.1.2.3")
		}
		if got := out.Header.Get("X-Forwarded-Proto"); got != "https" {
			t.Errorf("X-Forwarded-Proto = %q, want https", got)
		}
	})
}

func TestClientIP(t *testing.T) {
	tp := trusted("10.0.0.0/8", "192.168.1.1/32")
	cases := map[string]struct {
		remote, xff string
		want        string
	}{
		"untrusted peer ignores XFF": {"203.0.113.9:1", "198.51.100.7", "203.0.113.9"},
		"trusted peer uses XFF":      {"10.0.0.5:1", "198.51.100.7", "198.51.100.7"},
		"skips trusted hops":         {"10.0.0.5:1", "198.51.100.7, 192.168.1.1, 10.9.9.9", "198.51.100.7"},
		"spoofed left entry ignored": {"10.0.0.5:1", "1.2.3.4, 198.51.100.7", "198.51.100.7"},
		"trusted peer, no XFF":       {"10.0.0.5:1", "", "10.0.0.5"},
		"malformed hop stops walk":   {"10.0.0.5:1", "198.51.100.7, garbage", "10.0.0.5"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.remote
			if tc.xff != "" {
				r.Header.Set("X-Forwarded-For", tc.xff)
			}
			if got := tp.ClientIP(r); got != tc.want {
				t.Fatalf("ClientIP = %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("no trusted proxies", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = "10.0.0.5:1"
		r.Header.Set("X-Forwarded-For", "198.51.100.7")
		if got := TrustedProxies(nil).ClientIP(r); got != "10.0.0.5" {
			t.Fatalf("ClientIP = %q, want 10.0.0.5", got)
		}
	})
}
