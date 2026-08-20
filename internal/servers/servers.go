package servers

import (
	"fmt"
	"net/url"
)

var ports = []int{2115, 2215, 2315}

func ListURL() ([]*url.URL, error) {
	var backends []*url.URL
	for _, p := range ports {
		// Skip url.Parse() due to it not working without http(s):// prefix
		target := &url.URL{
			Scheme: "http",
			Host:   fmt.Sprintf("127.0.0.1:%d", p),
		}
		backends = append(backends, target)
	}
	return backends, nil
}