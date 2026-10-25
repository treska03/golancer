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
	"github.com/treska03/golancer/internal/metrics"
)

// Run loads the config, starts the registry and balancer servers plus the
// health prober, and blocks until interrupted, draining both servers on the
// way out.
func Run() {
	cfg := loadConfig()

	reg := backend.NewRegistry(cfg.DomainBackends())

	// Shared request metrics, registered once and mounted on every server via
	// the metrics middleware.
	metricsReg := metrics.NewRegistry()
	mw := commonMiddleware(metrics.NewCommon(metricsReg), cfg.ProxyBodyLimit())
	authMW := bearerAuth(cfg.RegistryToken())

	// Cancelled on SIGINT/SIGTERM; drives graceful shutdown of both servers.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup

	runProber(ctx, reg, cfg)                                   // active health probing; seeds health before any server listens
	startMetricsRegistryServer(ctx, &wg, metricsReg, mw, cfg)  // prometheus metrics registry
	startRegistryServer(ctx, &wg, reg, chain(mw, authMW), cfg) // backend (de)registration API (auth inside metrics)
	startBalancerServer(ctx, &wg, reg, mw, cfg)                // proxies traffic + health endpoint

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
	// as down for the first interval. Run calls this before starting any
	// server, so this blocking round must stay ahead of the listeners.
	// Runtime-added backends are picked up on the next probe round (up to one
	// interval later).
	prober.ProbeOnce(ctx)
	go prober.Run(ctx)
}
