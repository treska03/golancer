package bootstrap

import (
	"context"
	"log"
	"log/slog"
	"sync"
	"time"

	"github.com/treska03/golancer/internal/backend"
	"github.com/treska03/golancer/internal/config"
	"github.com/treska03/golancer/internal/handlers"
	"github.com/treska03/golancer/internal/kube"
	"github.com/treska03/golancer/internal/server"
)

// initialSyncWait bounds how long startup waits for the first endpoint set
// before serving anyway (e.g. API server unreachable); discovery keeps
// retrying in the background either way.
const initialSyncWait = 10 * time.Second

// startRegistryServer builds the registry server (the backend
// (de)registration API) and starts it in the background.
func startRegistryServer(ctx context.Context, wg *sync.WaitGroup, reg *backend.Registry, mw server.Middleware, cfg *config.Config) {
	regHandler := handlers.NewBackendHandler(reg)

	registryServer := server.New(cfg.RegistryConfig(), mw, regHandler)
	startServer(ctx, wg, registryServer)
}

// startDiscovery starts Kubernetes pod discovery when enabled in the config and
// waits (bounded) for the initial endpoint set, so the balancer doesn't start
// out with an empty pool. It exits the process on unusable configuration.
func startDiscovery(ctx context.Context, wg *sync.WaitGroup, reg *backend.Registry, cfg *config.Config) {
	if !cfg.KubeDiscoveryEnabled() {
		return
	}
	ks := cfg.Discovery.Kubernetes

	var (
		client *kube.Client
		err    error
	)
	if ks.APIServer != "" {
		client = kube.New(ks.APIServer)
	} else if client, err = kube.InCluster(); err != nil {
		log.Fatalf("kubernetes discovery: %v", err)
	}

	ns := ks.Namespace
	if ns == "" {
		if ns, err = kube.InClusterNamespace(); err != nil {
			log.Fatalf("kubernetes discovery: discovery.kubernetes.namespace unset and %v", err)
		}
	}

	d := backend.NewDiscovery(reg, &kube.Watcher{Client: client, Namespace: ns, Service: ks.Service}, ks.PortName)
	wg.Go(func() { d.Run(ctx) })

	select {
	case <-d.Synced():
	case <-time.After(initialSyncWait):
		slog.Warn("kubernetes discovery: initial sync not complete, starting anyway", "wait", initialSyncWait)
	case <-ctx.Done():
	}
}
