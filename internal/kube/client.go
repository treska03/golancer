// Package kube is a deliberately tiny Kubernetes API client: just enough to
// list and watch EndpointSlices over plain HTTPS+JSON using only the standard
// library. It avoids pulling k8s.io/client-go (and its large transitive
// dependency tree) into a program that needs exactly one watch.
package kube

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	saDir         = "/var/run/secrets/kubernetes.io/serviceaccount"
	tokenFile     = saDir + "/token"
	caFile        = saDir + "/ca.crt"
	namespaceFile = saDir + "/namespace"

	// tokenReload bounds how stale a projected ServiceAccount token may get.
	// The kubelet rotates bound tokens well before expiry (~80% of lifetime,
	// default 1h), so re-reading once a minute is plenty.
	tokenReload = time.Minute
)

// Client talks to a single Kubernetes API server.
type Client struct {
	base string // scheme://host[:port], no trailing slash
	http *http.Client
}

// InCluster builds a Client from the pod's ServiceAccount credentials and the
// KUBERNETES_SERVICE_HOST/PORT environment, the same way client-go does.
func InCluster() (*Client, error) {
	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, errors.New("not running in a cluster: KUBERNETES_SERVICE_HOST/PORT unset (set discovery.kubernetes.api-server to use e.g. `kubectl proxy`)")
	}

	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("reading cluster CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("no certificates found in %s", caFile)
	}

	// Fail early on a missing token rather than on the first request.
	src := &fileToken{path: tokenFile}
	if _, err := src.get(); err != nil {
		return nil, err
	}

	tr := newTransport(&tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12})
	return &Client{
		base: "https://" + net.JoinHostPort(host, port),
		http: &http.Client{Transport: &bearerTransport{base: tr, tok: src}},
	}, nil
}

// New builds an unauthenticated Client for apiServer, e.g. the
// "http://127.0.0.1:8001" served by `kubectl proxy`, which handles auth itself.
func New(apiServer string) *Client {
	return &Client{
		base: strings.TrimRight(apiServer, "/"),
		http: &http.Client{Transport: newTransport(nil)},
	}
}

// InClusterNamespace returns the namespace the pod runs in.
func InClusterNamespace() (string, error) {
	b, err := os.ReadFile(namespaceFile)
	if err != nil {
		return "", fmt.Errorf("reading pod namespace: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

// newTransport returns a transport suitable for long-lived watch streams: no
// overall timeout (callers bound requests with contexts), but bounded dial,
// TLS handshake and time-to-first-byte.
func newTransport(tlsCfg *tls.Config) *http.Transport {
	return &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:       tlsCfg,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          2,
		IdleConnTimeout:       90 * time.Second,
	}
}

// fileToken serves a bearer token from a file, re-reading it periodically so
// projected-token rotation is picked up without a restart.
type fileToken struct {
	path string

	mu      sync.Mutex
	token   string
	readAt  time.Time
	nowFunc func() time.Time // test hook
}

func (f *fileToken) get() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := time.Now
	if f.nowFunc != nil {
		now = f.nowFunc
	}
	if f.token != "" && now().Sub(f.readAt) < tokenReload {
		return f.token, nil
	}
	b, err := os.ReadFile(f.path)
	if err != nil {
		if f.token != "" {
			return f.token, nil // keep using the last good token
		}
		return "", fmt.Errorf("reading service account token: %w", err)
	}
	f.token, f.readAt = strings.TrimSpace(string(b)), now()
	return f.token, nil
}

type bearerTransport struct {
	base http.RoundTripper
	tok  *fileToken
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tok, err := t.tok.get()
	if err != nil {
		return nil, err
	}
	req = req.Clone(req.Context()) // RoundTrippers must not mutate the caller's request
	req.Header.Set("Authorization", "Bearer "+tok)
	return t.base.RoundTrip(req)
}
