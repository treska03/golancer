package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// handlerTransport serves requests from an http.Handler entirely in memory
// (no sockets), streaming the response body through a pipe so watch events
// reach the client as the handler flushes them.
type handlerTransport struct{ h http.Handler }

type pipeResponseWriter struct {
	hdr    http.Header
	pw     *io.PipeWriter
	status int
	ready  chan struct{}
	once   sync.Once
}

func (p *pipeResponseWriter) Header() http.Header { return p.hdr }
func (p *pipeResponseWriter) WriteHeader(code int) {
	p.once.Do(func() { p.status = code; close(p.ready) })
}
func (p *pipeResponseWriter) Write(b []byte) (int, error) { p.WriteHeader(200); return p.pw.Write(b) }
func (p *pipeResponseWriter) Flush()                      { p.WriteHeader(200) }

func (t handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	pr, pw := io.Pipe()
	rw := &pipeResponseWriter{hdr: http.Header{}, pw: pw, ready: make(chan struct{})}
	go func() {
		t.h.ServeHTTP(rw, req)
		rw.WriteHeader(200)
		pw.Close()
	}()
	go func() { <-req.Context().Done(); pr.CloseWithError(req.Context().Err()) }()
	select {
	case <-rw.ready:
		return &http.Response{StatusCode: rw.status, Status: fmt.Sprintf("%d %s", rw.status, http.StatusText(rw.status)),
			Header: rw.hdr, Body: pr, Request: req}, nil
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
}

func fakeClient(h http.Handler) *Client {
	return &Client{base: "http://fake", http: &http.Client{Transport: handlerTransport{h}}}
}

// fakeAPI is a minimal EndpointSlice API server. Each GET with watch=true pops
// the next scripted watch handler; LISTs return the current list.
type fakeAPI struct {
	mu       sync.Mutex
	list     string // JSON body for LIST
	watches  []func(w http.ResponseWriter, r *http.Request)
	lists    int
	queries  []string // raw queries of watch requests
	selector string
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path != "/apis/discovery.k8s.io/v1/namespaces/ns/endpointslices" {
		http.NotFound(w, r)
		return
	}
	f.selector = r.URL.Query().Get("labelSelector")
	if r.URL.Query().Get("watch") != "true" {
		f.lists++
		fmt.Fprint(w, f.list)
		return
	}
	f.queries = append(f.queries, r.URL.RawQuery)
	if len(f.watches) == 0 {
		f.mu.Unlock()
		defer f.mu.Lock()
		<-r.Context().Done() // idle watch; must not hold the lock while blocked
		return
	}
	h := f.watches[0]
	f.watches = f.watches[1:]
	f.mu.Unlock()
	defer f.mu.Lock()
	h(w, r)
}

func sliceJSON(name, rv string, ips ...string) string {
	eps := ""
	for i, ip := range ips {
		if i > 0 {
			eps += ","
		}
		eps += fmt.Sprintf(`{"addresses":[%q],"conditions":{"ready":true}}`, ip)
	}
	return fmt.Sprintf(`{"metadata":{"name":%q,"resourceVersion":%q},"addressType":"IPv4","endpoints":[%s],"ports":[{"name":"http","port":8080,"protocol":"TCP"}]}`,
		name, rv, eps)
}

func event(typ, obj string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"type":%q,"object":%s}`+"\n", typ, obj)
	}
}

func seq(hs ...func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		for _, h := range hs {
			h(w, r)
			w.(http.Flusher).Flush()
		}
	}
}

func names(ss []EndpointSlice) []string {
	var out []string
	for _, s := range ss {
		out = append(out, s.Metadata.Name)
	}
	slices.Sort(out)
	return out
}

func run(t *testing.T, api *fakeAPI) <-chan []EndpointSlice {
	t.Helper()
	out := make(chan []EndpointSlice, 16)
	w := &Watcher{
		Client: fakeClient(api), Namespace: "ns", Service: "web",
		WatchTimeout: time.Minute,
		MinBackoff:   5 * time.Millisecond, MaxBackoff: 20 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Watch(ctx, func(s []EndpointSlice) { out <- s }); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return out
}

func next(t *testing.T, ch <-chan []EndpointSlice) []string {
	t.Helper()
	select {
	case s := <-ch:
		return names(s)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for onChange")
		return nil
	}
}

func TestWatcherListThenEvents(t *testing.T) {
	// Hold the watch stream open so events trickle in after the list.
	release := make(chan struct{})
	api := &fakeAPI{
		list: `{"metadata":{"resourceVersion":"10"},"items":[` + sliceJSON("a", "9", "10.0.0.1") + `]}`,
		watches: []func(http.ResponseWriter, *http.Request){
			seq(
				event("ADDED", sliceJSON("b", "11", "10.0.0.2")),
				event("BOOKMARK", `{"metadata":{"resourceVersion":"12"}}`),
				event("MODIFIED", sliceJSON("a", "13", "10.0.0.1", "10.0.0.3")),
				event("DELETED", sliceJSON("b", "14")),
				func(w http.ResponseWriter, r *http.Request) { <-release },
			),
		},
	}
	defer close(release)
	ch := run(t, api)

	for i, want := range [][]string{{"a"}, {"a", "b"}, {"a", "b"}, {"a"}} {
		if got := next(t, ch); !slices.Equal(got, want) {
			t.Fatalf("emit %d: got %v, want %v", i, got, want)
		}
	}
	if api.selector != "kubernetes.io/service-name=web" {
		t.Fatalf("unexpected label selector %q", api.selector)
	}
}

func TestWatcherResumesFromLastResourceVersion(t *testing.T) {
	api := &fakeAPI{
		list: `{"metadata":{"resourceVersion":"10"},"items":[]}`,
		watches: []func(http.ResponseWriter, *http.Request){
			// First watch yields one event, then ends cleanly after >1s (a
			// normal server-side timeout).
			func(w http.ResponseWriter, r *http.Request) {
				event("ADDED", sliceJSON("a", "11", "10.0.0.1"))(w, r)
				w.(http.Flusher).Flush()
				time.Sleep(1100 * time.Millisecond)
			},
		},
	}
	ch := run(t, api)
	next(t, ch) // initial empty
	next(t, ch) // ADDED a

	deadline := time.Now().Add(5 * time.Second)
	for {
		api.mu.Lock()
		n, lists := len(api.queries), api.lists
		var q string
		if n >= 2 {
			q = api.queries[1]
		}
		api.mu.Unlock()
		if n >= 2 {
			if lists != 1 {
				t.Fatalf("resume must not relist, lists=%d", lists)
			}
			if !strings.Contains(q, "resourceVersion=11") || !strings.Contains(q, "allowWatchBookmarks=true") {
				t.Fatalf("second watch should resume from rv 11 with bookmarks: %s", q)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("watch was not resumed")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWatcherRelistsOnGone(t *testing.T) {
	status, _ := json.Marshal(map[string]any{"kind": "Status", "code": 410, "reason": "Expired", "message": "too old resource version"})
	api := &fakeAPI{
		list: `{"metadata":{"resourceVersion":"10"},"items":[` + sliceJSON("a", "9", "10.0.0.1") + `]}`,
		watches: []func(http.ResponseWriter, *http.Request){
			event("ERROR", string(status)),
		},
	}
	ch := run(t, api)
	if got := next(t, ch); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("initial: %v", got)
	}

	// Change what a relist returns, then expect it to be emitted again.
	api.mu.Lock()
	api.list = `{"metadata":{"resourceVersion":"20"},"items":[` + sliceJSON("z", "19", "10.0.0.9") + `]}`
	api.mu.Unlock()
	if got := next(t, ch); !slices.Equal(got, []string{"z"}) {
		t.Fatalf("after 410 relist: got %v, want [z]", got)
	}
}

func TestWatcherRetriesListFailure(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("watch") == "true" {
			<-r.Context().Done()
			return
		}
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n < 3 {
			http.Error(w, `{"code":403,"message":"forbidden"}`, http.StatusForbidden)
			return
		}
		fmt.Fprint(w, `{"metadata":{"resourceVersion":"1"},"items":[`+sliceJSON("a", "1", "10.0.0.1")+`]}`)
	})

	out := make(chan []EndpointSlice, 4)
	w := &Watcher{Client: fakeClient(h), Namespace: "ns", Service: "web",
		MinBackoff: 5 * time.Millisecond, MaxBackoff: 10 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Watch(ctx, func(s []EndpointSlice) { out <- s }); close(done) }()
	defer func() { cancel(); <-done }()

	if got := next(t, out); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("got %v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 3 {
		t.Fatalf("expected 2 failures then success, calls=%d", calls)
	}
}

// The API server answers a fractional timeoutSeconds with 400 Bad Request, and
// the default watch lifetime is randomized, so it must be sent as an integer.
func TestWatcherSendsWholeTimeoutSeconds(t *testing.T) {
	cases := map[string]struct {
		timeout  time.Duration
		min, max int
	}{
		"randomized default": {0, 300, 599},
		"fractional":         {90*time.Second + 700*time.Millisecond, 90, 90},
		"sub-second":         {300 * time.Millisecond, 1, 1},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			api := &fakeAPI{list: `{"metadata":{"resourceVersion":"1"},"items":[]}`}
			w := &Watcher{Client: fakeClient(api), Namespace: "ns", Service: "web", WatchTimeout: tc.timeout}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { _ = w.Watch(ctx, func([]EndpointSlice) {}); close(done) }()
			defer func() { cancel(); <-done }()

			var query string
			for deadline := time.Now().Add(5 * time.Second); query == "" && time.Now().Before(deadline); {
				api.mu.Lock()
				if len(api.queries) > 0 {
					query = api.queries[0]
				}
				api.mu.Unlock()
				time.Sleep(time.Millisecond)
			}
			if query == "" {
				t.Fatal("no watch request was made")
			}
			q, err := url.ParseQuery(query)
			if err != nil {
				t.Fatal(err)
			}
			raw := q.Get("timeoutSeconds")
			got, err := strconv.Atoi(raw)
			if err != nil {
				t.Fatalf("timeoutSeconds %q is not an integer", raw)
			}
			if got < tc.min || got > tc.max {
				t.Fatalf("timeoutSeconds = %d, want %d..%d", got, tc.min, tc.max)
			}
		})
	}
}

func TestFileTokenReload(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/token"
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Unix(1000, 0)
	ft := &fileToken{path: path, nowFunc: func() time.Time { return now }}

	write("one\n")
	if got, err := ft.get(); err != nil || got != "one" {
		t.Fatalf("got %q, %v", got, err)
	}
	write("two")
	if got, _ := ft.get(); got != "one" {
		t.Fatalf("token should be cached, got %q", got)
	}
	now = now.Add(2 * tokenReload)
	if got, _ := ft.get(); got != "two" {
		t.Fatalf("token should have been reloaded, got %q", got)
	}
}
