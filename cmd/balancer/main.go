package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/treska03/golancer/internal/backend"
	"github.com/treska03/golancer/internal/health"
	"github.com/treska03/golancer/internal/server"
)

func main() {
	// 0. Load config: baseline backends and health-probing settings.
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	// 0. Load config: baseline backends and health-probing settings.
	cfg := loadConfig(*cfgPath)

	backends, err := cfg.DomainBackends()
	if err != nil {
		log.Fatalf("Failed to parse backends: %v", err)
	}

	healthCfg, err := cfg.HealthConfig()
	if err != nil {
		log.Fatalf("Failed to parse health config: %v", err)
	}

	registry := backend.NewRegistry(backends)
	pool := cfg.ProxySelector(registry)

	scfg, err := cfg.ServerConfig()
	if err != nil {
		log.Fatalf("Failed to parse proxy config: %v", err)
	}
	// 2. Build http server
	s := server.New(registry, pool, scfg)

	// 3. Active health probing. Backends start unhealthy; the prober marks them
	//    up once they answer a probe and evicts them when they stop. Cancelled on
	//    SIGINT/SIGTERM together with server shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	prober := health.NewProber(registry, healthCfg)
	// Seed health before we accept traffic so baseline backends aren't treated
	// as down for the first interval. Runtime-added backends are picked up on
	// the next probe round (up to one interval later).
	prober.ProbeOnce(ctx)
	go prober.Run(ctx)

	// Graceful shutdown: when signalled, stop accepting connections and let
	// in-flight requests drain. Closing the server unblocks ListenAndServe.
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.Shutdown(shutCtx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
	}()

	// 4. Start Listening
	log.Printf("Load balancer running on %s with %d baseline backend(s)", s.Addr, len(backends))
	if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
	log.Println("load balancer stopped")
}
