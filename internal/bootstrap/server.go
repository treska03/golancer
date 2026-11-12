package bootstrap

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/treska03/golancer/internal/backend"
	"github.com/treska03/golancer/internal/config"
	"github.com/treska03/golancer/internal/handlers"
	"github.com/treska03/golancer/internal/proxy"
	"github.com/treska03/golancer/internal/server"
)

// startMetricsRegistryServer builds the prometheus metrics server
func startMetricsRegistryServer(ctx context.Context, wg *sync.WaitGroup, reg *prometheus.Registry, mw server.Middleware, cfg *config.Config) {
	regHandler := handlers.NewPrometheusHandler(reg, cfg.MetricsConfig())
	regServer := server.New(cfg.MetricsConfig(), mw, regHandler)

	startServer(ctx, wg, regServer)
}

// startBalancerServer builds the load-balancer server (proxy + health endpoint)
// and starts it in the background.
func startBalancerServer(ctx context.Context, wg *sync.WaitGroup, reg *backend.Registry, mw server.Middleware, cfg *config.Config) {
	pool := cfg.ProxySelector(reg)
	lbHandler := proxy.NewHandler(pool, cfg.Balancer.MaxRetries, cfg.TrustedProxies())
	healthHandler := handlers.NewHealthHandler(reg)

	balancerServer := server.New(cfg.BalancerConfig(), mw, lbHandler, healthHandler)
	startServer(ctx, wg, balancerServer)
}

// startServer runs srv in the background and shuts it down gracefully when ctx
// is cancelled. wg tracks both the listener and the shutdown goroutine, so it is
// only done once in-flight requests have drained (or the drain timed out).
func startServer(ctx context.Context, wg *sync.WaitGroup, srv *http.Server) {
	// Graceful shutdown: when signalled, stop accepting connections and let
	// in-flight requests drain. ListenAndServe returns as soon as Shutdown is
	// called, not when draining finishes, so this goroutine must be waited on
	// too or the process would exit mid-drain.
	wg.Go(func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
	})

	wg.Go(func() {
		log.Printf("Listener running on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
		log.Printf("server on %s stopped", srv.Addr)
	})
}
