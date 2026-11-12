package backend

import (
	"context"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/treska03/golancer/internal/kube"
)

func ptr[T any](v T) *T { return &v }

func slice(name string, ready []*bool, ips ...string) kube.EndpointSlice {
	s := kube.EndpointSlice{
		Metadata:    kube.ObjectMeta{Name: name},
		AddressType: "IPv4",
		Ports:       []kube.EndpointPort{{Name: ptr("http"), Port: ptr[int32](8080), Protocol: ptr("TCP")}},
	}
	for i, ip := range ips {
		var r *bool
		if i < len(ready) {
			r = ready[i]
		}
		s.Endpoints = append(s.Endpoints, kube.Endpoint{
			Addresses:  []string{ip},
			Conditions: kube.EndpointConditions{Ready: r},
		})
	}
	return s
}

var (
	yes, no = ptr(true), ptr(false)
)

func TestAddressesFromSlices(t *testing.T) {
	list := []kube.EndpointSlice{
		slice("a", []*bool{yes, no, nil}, "10.0.0.2", "10.0.0.3", "10.0.0.4"), // not-ready dropped, nil ready kept
		slice("b", []*bool{yes, yes}, "10.0.0.1", "10.0.0.2"),                 // .2 duplicated across slices
	}
	want := []string{"10.0.0.1:8080", "10.0.0.2:8080", "10.0.0.4:8080"}
	if got := addressesFromSlices(list, "http"); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := addressesFromSlices(list, "nope"); len(got) != 0 {
		t.Fatalf("unknown port name should yield nothing, got %v", got)
	}
}

func TestAddressesFromSlicesPortsAndFamilies(t *testing.T) {
	v6 := slice("v6", []*bool{yes}, "fd00::1")
	v6.AddressType = "IPv6"
	fqdn := slice("fqdn", []*bool{yes}, "example.com")
	fqdn.AddressType = "FQDN"
	udp := slice("udp", []*bool{yes}, "10.0.0.9")
	udp.Ports[0].Protocol = ptr("UDP")
	unnamed := slice("unnamed", []*bool{yes}, "10.0.0.5")
	unnamed.Ports[0].Name = nil
	noProto := slice("noproto", []*bool{yes}, "10.0.0.6")
	noProto.Ports[0].Protocol = nil
	noProto.Ports[0].Name = nil

	got := addressesFromSlices([]kube.EndpointSlice{v6, fqdn, udp, unnamed, noProto}, "")
	want := []string{"10.0.0.5:8080", "10.0.0.6:8080"}
	if !slices.Equal(got, want) {
		t.Fatalf("unnamed port: got %v, want %v", got, want)
	}

	got = addressesFromSlices([]kube.EndpointSlice{v6, fqdn, udp}, "http")
	if want := []string{"[fd00::1]:8080"}; !slices.Equal(got, want) {
		t.Fatalf("named port: got %v, want %v", got, want)
	}
}

// fakeSource lets a test push snapshots through Discovery.
type fakeSource struct{ push chan []kube.EndpointSlice }

func (f *fakeSource) Watch(ctx context.Context, onChange func([]kube.EndpointSlice)) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case s := <-f.push:
			onChange(s)
		}
	}
}

func urls(r *Registry) []string {
	var out []string
	for _, b := range r.ListBackends() {
		out = append(out, b.URL.Host)
	}
	slices.Sort(out)
	return out
}

func TestDiscoveryReconcile(t *testing.T) {
	static, _ := url.Parse("http://10.0.0.1:8080") // overlaps a discovered pod
	other, _ := url.Parse("http://static.example:80")
	reg := NewRegistry(nil)
	if _, err := reg.AddNewBackend(static, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.AddNewBackend(other, 1); err != nil {
		t.Fatal(err)
	}

	src := &fakeSource{push: make(chan []kube.EndpointSlice)}
	d := NewDiscovery(reg, src, "http")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	push := func(s ...kube.EndpointSlice) {
		t.Helper()
		select {
		case src.push <- s:
		case <-time.After(2 * time.Second):
			t.Fatal("discovery not consuming")
		}
	}

	select {
	case <-d.Synced():
		t.Fatal("synced before first snapshot")
	default:
	}

	push(slice("a", []*bool{yes, yes}, "10.0.0.1", "10.0.0.2"))
	<-d.Synced()
	// .1 already existed statically and must not be duplicated.
	if got, want := urls(reg), []string{"10.0.0.1:8080", "10.0.0.2:8080", "static.example:80"}; !slices.Equal(got, want) {
		t.Fatalf("after initial: got %v, want %v", got, want)
	}
	for _, b := range reg.ListBackends() {
		if b.URL.Host == "10.0.0.2:8080" && !b.Healthy() {
			t.Fatal("discovered backend should be admitted as healthy immediately")
		}
	}

	// Keep the instance ID of an unchanged pod across reconciles.
	var id2 string
	for _, b := range reg.ListBackends() {
		if b.URL.Host == "10.0.0.2:8080" {
			id2 = b.InstanceID
		}
	}

	// Scale up, then .1 fails readiness (statically owned: must stay).
	push(slice("a", []*bool{no, yes}, "10.0.0.1", "10.0.0.2"), slice("b", []*bool{yes}, "10.0.0.3"))
	// Barrier: the source is unbuffered, so once a following push is accepted
	// the previous onChange has completed.
	push(slice("a", []*bool{no, yes}, "10.0.0.1", "10.0.0.2"), slice("b", []*bool{yes}, "10.0.0.3"))
	if got, want := urls(reg), []string{"10.0.0.1:8080", "10.0.0.2:8080", "10.0.0.3:8080", "static.example:80"}; !slices.Equal(got, want) {
		t.Fatalf("after scale-up: got %v, want %v", got, want)
	}

	// Everything goes away: discovered pods removed, static ones kept.
	push()
	push()
	if got, want := urls(reg), []string{"10.0.0.1:8080", "static.example:80"}; !slices.Equal(got, want) {
		t.Fatalf("after empty: got %v, want %v", got, want)
	}

	// Pod returns: same address is re-added with a fresh instance.
	push(slice("a", []*bool{yes}, "10.0.0.2"))
	push(slice("a", []*bool{yes}, "10.0.0.2"))
	for _, b := range reg.ListBackends() {
		if b.URL.Host == "10.0.0.2:8080" && b.InstanceID == id2 {
			t.Fatal("expected a new instance ID after removal and re-add")
		}
	}
}
