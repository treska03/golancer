package backend

import (
	"context"
	"log/slog"
	"net/url"
	"slices"
	"sync"

	"github.com/treska03/golancer/internal/kube"
)

// SliceSource streams the complete current set of EndpointSlices for a
// Service to onChange: once after the initial LIST, then after each change.
// It blocks until ctx is done. *kube.Watcher implements it.
type SliceSource interface {
	Watch(ctx context.Context, onChange func([]kube.EndpointSlice)) error
}

// discoveredWeight is the weight given to every discovered pod. Pods of one
// Service are interchangeable, so they are balanced evenly.
const discoveredWeight = 1

// Discovery mirrors a Kubernetes Service's ready pod endpoints into a Registry.
//
// It only ever removes backends it added itself, so statically configured and
// self-registered backends are left alone.
type Discovery struct {
	reg      *Registry
	src      SliceSource
	portName string
	scheme   string

	synced   chan struct{} // closed after the first reconcile
	syncOnce sync.Once

	// Only touched from the single goroutine running Run (onChange is invoked
	// synchronously by the source), so no locking is needed.
	owned map[string]string // "host:port" -> instance ID
	last  []string          // last reconciled address set, sorted
	have  bool              // last is valid
}

// NewDiscovery builds a Discovery that publishes the ready endpoints exposing
// portName (see addressesFromSlices) from src into reg.
func NewDiscovery(reg *Registry, src SliceSource, portName string) *Discovery {
	return &Discovery{
		reg:      reg,
		src:      src,
		portName: portName,
		scheme:   "http",
		synced:   make(chan struct{}),
		owned:    map[string]string{},
	}
}

// Synced is closed once the initial endpoint set has been loaded into the
// registry, so callers can hold off serving traffic until it is populated.
func (d *Discovery) Synced() <-chan struct{} { return d.synced }

// Run watches until ctx is cancelled.
func (d *Discovery) Run(ctx context.Context) {
	if err := d.src.Watch(ctx, d.onChange); err != nil && ctx.Err() == nil {
		slog.Error("pod discovery stopped", "err", err)
	}
}

// onChange recomputes the desired backend set from the full slice set and
// reconciles the registry. It is a no-op when the address set is unchanged
// (EndpointSlices are rewritten often for reasons that don't affect us, e.g.
// topology hints), which also avoids needless registry churn.
func (d *Discovery) onChange(snapshot []kube.EndpointSlice) {
	want := addressesFromSlices(snapshot, d.portName)

	if d.have && slices.Equal(want, d.last) {
		return
	}
	d.last, d.have = want, true
	d.reconcile(want)
	d.syncOnce.Do(func() { close(d.synced) })
}

func (d *Discovery) reconcile(want []string) {
	wantSet := make(map[string]struct{}, len(want))
	for _, addr := range want {
		wantSet[addr] = struct{}{}
	}

	for addr, id := range d.owned {
		if _, ok := wantSet[addr]; !ok {
			d.reg.RemoveInstanceByID(id)
			delete(d.owned, addr)
			slog.Info("discovery: backend removed", "addr", addr)
		}
	}

	for _, addr := range want {
		if _, ok := d.owned[addr]; ok {
			continue
		}
		id, err := d.reg.AddNewBackend(&url.URL{Scheme: d.scheme, Host: addr}, discoveredWeight)
		if err != nil {
			// Most likely the same address is already registered statically.
			slog.Debug("discovery: not adding backend", "addr", addr, "err", err)
			continue
		}
		// The pod passed its readiness probe (that is what ready=true means),
		// so admit it immediately instead of waiting up to one health-probe
		// interval. The active prober still evicts it if it stops responding.
		d.reg.SetHealthy(id, true)
		d.owned[addr] = id
		slog.Info("discovery: backend added", "addr", addr)
	}
}
