package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// LabelServiceName is set by the EndpointSlice controller on every slice and
// names the Service that owns it.
const LabelServiceName = "kubernetes.io/service-name"

// The structs below mirror only the discovery.k8s.io/v1 fields we use.

type EndpointSlice struct {
	Metadata    ObjectMeta     `json:"metadata"`
	AddressType string         `json:"addressType"`
	Endpoints   []Endpoint     `json:"endpoints"`
	Ports       []EndpointPort `json:"ports"`
}

type ObjectMeta struct {
	Name            string `json:"name"`
	ResourceVersion string `json:"resourceVersion"`
}

type Endpoint struct {
	Addresses  []string           `json:"addresses"`
	Conditions EndpointConditions `json:"conditions"`
}

// EndpointConditions are pointers because nil means "unknown". Per the API
// contract a nil Ready MUST be interpreted as ready.
type EndpointConditions struct {
	Ready       *bool `json:"ready"`
	Serving     *bool `json:"serving"`
	Terminating *bool `json:"terminating"`
}

type EndpointPort struct {
	Name     *string `json:"name"`
	Port     *int32  `json:"port"`
	Protocol *string `json:"protocol"`
}

type sliceList struct {
	Metadata ObjectMeta      `json:"metadata"`
	Items    []EndpointSlice `json:"items"`
}

type watchEvent struct {
	Type   string          `json:"type"`
	Object json.RawMessage `json:"object"`
}

// status is the subset of metav1.Status returned inside ERROR watch events and
// non-2xx responses.
type status struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Reason  string `json:"reason"`
}

// errGone means our resourceVersion is too old; the caller must relist.
var errGone = errors.New("resourceVersion expired (410 Gone)")

// Watcher keeps a local copy of the EndpointSlices that belong to one Service
// and reports the full set whenever it changes. It does the same job as a
// client-go reflector + informer: LIST, then WATCH from the list's
// resourceVersion, resuming on timeouts and relisting on 410 Gone or errors.
type Watcher struct {
	Client    *Client
	Namespace string
	Service   string

	// WatchTimeout is the server-side watch lifetime (default 5-10 minutes,
	// randomized so replicas don't reconnect in lockstep).
	WatchTimeout time.Duration
	// MinBackoff and MaxBackoff bound the retry delay after a failure
	// (defaults 1s and 30s).
	MinBackoff, MaxBackoff time.Duration
}

func (w *Watcher) backoffBounds() (lo, hi time.Duration) {
	lo, hi = w.MinBackoff, w.MaxBackoff
	if lo <= 0 {
		lo = time.Second
	}
	if hi < lo {
		hi = max(30*time.Second, lo)
	}
	return lo, hi
}

func (w *Watcher) watchTimeout() time.Duration {
	d := w.WatchTimeout
	if d <= 0 {
		d = 5*time.Minute + rand.N(5*time.Minute)
	}
	return max(d.Truncate(time.Second), time.Second)
}

// Watch blocks until ctx is cancelled. onChange receives the complete current
// slice set (in no particular order) after the initial LIST and after every
// subsequent change. The slice passed to onChange is owned by the callee.
// Transient API errors are retried with backoff and never returned; the last
// known state is simply retained until the API server is reachable again.
func (w *Watcher) Watch(ctx context.Context, onChange func([]EndpointSlice)) error {
	lo, hi := w.backoffBounds()
	backoff := lo

	for ctx.Err() == nil {
		progressed, err := w.listAndWatch(ctx, onChange)
		if ctx.Err() != nil {
			break
		}
		if progressed {
			backoff = lo // we made progress before failing; retry promptly
		}
		slog.Warn("endpointslice watch ended, retrying", "err", err, "in", backoff)

		select {
		case <-ctx.Done():
		case <-time.After(backoff + rand.N(backoff/2+1)):
		}
		backoff = min(backoff*2, hi)
	}
	return ctx.Err()
}

// listAndWatch performs one LIST and then watches until the watch fails in a
// way that requires a relist. progressed reports whether the LIST succeeded.
func (w *Watcher) listAndWatch(ctx context.Context, onChange func([]EndpointSlice)) (progressed bool, err error) {
	list, err := w.list(ctx)
	if err != nil {
		return false, fmt.Errorf("list: %w", err)
	}

	state := make(map[string]EndpointSlice, len(list.Items))
	for _, s := range list.Items {
		state[s.Metadata.Name] = s
	}
	rv := list.Metadata.ResourceVersion
	emit := func() {
		out := make([]EndpointSlice, 0, len(state))
		for _, s := range state {
			out = append(out, s)
		}
		onChange(out)
	}
	emit()

	// Resume watches (they end on every server timeout) until something fails.
	for ctx.Err() == nil {
		started := time.Now()
		err := w.watch(ctx, &rv, state, emit)
		switch {
		case err == nil && time.Since(started) < time.Second:
			// The server closed the stream right away; resuming in a tight
			// loop would hammer it. Fall back to the backoff/relist path.
			return true, errors.New("watch closed immediately")
		case err == nil:
			continue // clean server-side timeout: resume from rv
		case errors.Is(err, errGone):
			return true, err // relist
		default:
			return true, fmt.Errorf("watch: %w", err)
		}
	}
	return true, ctx.Err()
}

func (w *Watcher) sliceURL(q url.Values) string {
	q.Set("labelSelector", LabelServiceName+"="+w.Service)
	return fmt.Sprintf("%s/apis/discovery.k8s.io/v1/namespaces/%s/endpointslices?%s",
		w.Client.base, url.PathEscape(w.Namespace), q.Encode())
}

func (w *Watcher) list(ctx context.Context) (*sliceList, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := w.do(ctx, w.sliceURL(url.Values{}))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var l sliceList
	if err := json.NewDecoder(resp.Body).Decode(&l); err != nil {
		return nil, fmt.Errorf("decoding list: %w", err)
	}
	return &l, nil
}

// watch streams events starting after *rv, applying them to state and calling
// emit after each change. It updates *rv as it goes and returns nil when the
// stream ends cleanly (server timeout) so the caller can resume.
func (w *Watcher) watch(parent context.Context, rv *string, state map[string]EndpointSlice, emit func()) error {
	timeout := w.watchTimeout()

	// The server closes the stream after timeoutSeconds; this client-side
	// deadline (slightly longer) rescues us from half-open TCP connections.
	ctx, cancel := context.WithTimeout(parent, timeout+30*time.Second)
	defer cancel()

	resp, err := w.do(ctx, w.sliceURL(url.Values{
		"watch":               {"true"},
		"resourceVersion":     {*rv},
		"allowWatchBookmarks": {"true"},
		"timeoutSeconds":      {strconv.FormatInt(int64(timeout/time.Second), 10)},
	}))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	for {
		var ev watchEvent
		if err := dec.Decode(&ev); err != nil {
			// A clean EOF (server timeout) or our own deadline is the normal
			// end of a watch, not a failure. Cancellation of the parent is
			// surfaced to the caller as an error so it stops.
			if errors.Is(err, io.EOF) || (parent.Err() == nil && ctx.Err() != nil) {
				return nil
			}
			return err
		}

		if ev.Type == "ERROR" {
			var st status
			_ = json.Unmarshal(ev.Object, &st)
			if st.Code == http.StatusGone {
				return errGone
			}
			return fmt.Errorf("watch error event: %d %s", st.Code, st.Message)
		}

		var s EndpointSlice
		if err := json.Unmarshal(ev.Object, &s); err != nil {
			return fmt.Errorf("decoding %s event: %w", ev.Type, err)
		}
		if s.Metadata.ResourceVersion != "" {
			*rv = s.Metadata.ResourceVersion
		}

		switch ev.Type {
		case "ADDED", "MODIFIED":
			state[s.Metadata.Name] = s
			emit()
		case "DELETED":
			if _, ok := state[s.Metadata.Name]; ok {
				delete(state, s.Metadata.Name)
				emit()
			}
		case "BOOKMARK":
			// Only advances rv, which was done above.
		}
	}
}

// do issues a GET and converts non-2xx responses into errors (errGone for 410).
func (w *Watcher) do(ctx context.Context, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := w.Client.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 300 {
		return resp, nil
	}
	defer resp.Body.Close()

	var st status
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&st)
	if resp.StatusCode == http.StatusGone {
		return nil, errGone
	}
	return nil, fmt.Errorf("%s: %d %s", resp.Status, st.Code, st.Message)
}
