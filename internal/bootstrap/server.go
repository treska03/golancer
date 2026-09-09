package bootstrap

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/treska03/golancer/internal/backend"
	"github.com/treska03/golancer/internal/config"
	"github.com/treska03/golancer/internal/handlers"
	"github.com/treska03/golancer/internal/proxy"
	"github.com/treska03/golancer/internal/server"
)

func bootstrapServer(ctx context.Context, reg *backend.Registry, cfg *config.Config) {
	pool := cfg.ProxySelector(reg)
	lbHandler := proxy.NewHandler(pool, cfg.Server.Balancer.MaxRetries) // todo: think about how we access it
	healthHandler := handlers.NewHealthHandler(reg)

	balancerServer := server.New(cfg.BalancerConfig(), lbHandler, healthHandler)

	// Graceful shutdown: when signalled, stop accepting connections and let
	// in-flight requests drain. Closing the server unblocks ListenAndServe.
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := balancerServer.Shutdown(shutCtx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
	}()

	go func() {
		log.Printf("Listener running on %s", balancerServer.Addr)
		if err := balancerServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
		log.Println("load balancer stopped")
	}()
}
