package bootstrap

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/treska03/golancer/internal/backend"
	"github.com/treska03/golancer/internal/config"
	"github.com/treska03/golancer/internal/handlers"
	"github.com/treska03/golancer/internal/health"
	"github.com/treska03/golancer/internal/server"
)

func Run() {
	// 0. Load config: baseline backends and health-probing settings.
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	// 0. Load config: baseline backends and health-probing settings.
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("Failed to parse config: %v", err)
	}

	reg := backend.NewRegistry(cfg.DomainBackends())

	regHandler := handlers.NewBackendHandler(reg)

	// 2. Build main load balancer server and registry
	registryServer := server.New(cfg.RegistryConfig(), regHandler)

	// 3. Active health probing. Backends start unhealthy; the prober marks them
	//    up once they answer a probe and evicts them when they stop. Cancelled on
	//    SIGINT/SIGTERM together with server shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	bootstrapServer(ctx, reg, cfg)
	bootstrapServer(ctx, reg, cfg)
	runProber(ctx, reg, cfg)
}

func runProber(ctx context.Context, reg *backend.Registry, cfg *config.Config) {
	prober := health.NewProber(reg, cfg.HealthConfig())
	// Seed health before we accept traffic so baseline backends aren't treated
	// as down for the first interval. Runtime-added backends are picked up on
	// the next probe round (up to one interval later).
	prober.ProbeOnce(ctx)
	go prober.Run(ctx)
}
