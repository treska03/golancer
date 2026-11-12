package backend

import (
	"net"
	"slices"
	"strconv"

	"github.com/treska03/golancer/internal/kube"
)

// addressesFromSlices flattens EndpointSlices into a sorted, de-duplicated list
// of "host:port" strings for the named port.
//
// Rules, per the discovery.k8s.io/v1 contract:
//   - an endpoint counts only if conditions.ready is true, and a nil ready
//     means true (so terminating pods, which report ready=false, drop out);
//   - only IPv4/IPv6 slices are used; FQDN slices are skipped;
//   - portName "" matches the unnamed port (EndpointSlice reports it as nil
//     or ""); only TCP ports match (a nil protocol defaults to TCP);
//   - the same pod IP may legitimately appear in several slices while the
//     controller reshuffles them, hence the de-duplication.
func addressesFromSlices(list []kube.EndpointSlice, portName string) []string {
	var out []string
	for _, s := range list {
		if s.AddressType != "IPv4" && s.AddressType != "IPv6" {
			continue
		}
		port, ok := matchPort(s.Ports, portName)
		if !ok {
			continue
		}
		for _, ep := range s.Endpoints {
			if ep.Conditions.Ready != nil && !*ep.Conditions.Ready {
				continue
			}
			for _, addr := range ep.Addresses {
				out = append(out, net.JoinHostPort(addr, strconv.Itoa(int(port))))
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func matchPort(ports []kube.EndpointPort, name string) (int32, bool) {
	for _, p := range ports {
		if p.Port == nil {
			continue
		}
		if p.Protocol != nil && *p.Protocol != "TCP" {
			continue
		}
		got := ""
		if p.Name != nil {
			got = *p.Name
		}
		if got == name {
			return *p.Port, true
		}
	}
	return 0, false
}
