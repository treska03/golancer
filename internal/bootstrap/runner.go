package bootstrap

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"sync"
	"syscall"

	"github.com/treska03/golancer/internal/backend"
	"github.com/treska03/golancer/internal/config"
	"github.com/treska03/golancer/internal/health"
)

// Run loads the config, starts the registry and balancer servers plus the
// health prober, and blocks until interrupted, draining both servers on the
// way out.
func Run() {
	cfg := loadConfig()

	reg := backend.NewRegistry(cfg.DomainBackends())

	// Cancelled on SIGINT/SIGTERM; drives graceful shutdown of both servers.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup

	startRegistryServer(ctx, &wg, reg, cfg) // backend (de)registration API
	startBalancerServer(ctx, &wg, reg, cfg) // proxies traffic + health endpoint
	runProber(ctx, reg, cfg)                // active health probing

	// Block until signalled and both servers have finished draining.
	wg.Wait()
}

// loadConfig parses the -config flag and loads the config file, exiting the
// process if it can't be read or is invalid.
func loadConfig() *config.Config {
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("Failed to parse config: %v", err)
	}
	return cfg
}

func runProber(ctx context.Context, reg *backend.Registry, cfg *config.Config) {
	prober := health.NewProber(reg, cfg.HealthConfig())
	// Seed health before we accept traffic so baseline backends aren't treated
	// as down for the first interval. Runtime-added backends are picked up on
	// the next probe round (up to one interval later).
	prober.ProbeOnce(ctx)
	go prober.Run(ctx)
}
