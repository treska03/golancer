package main

import (
	"log"
	"net/http"

	"github.com/treska03/golancer/internal/backend"
	"github.com/treska03/golancer/internal/balancer"
	"github.com/treska03/golancer/internal/handlers"
	"github.com/treska03/golancer/internal/proxy"
)

func main() {
	// 0. Load baseline backends from config
	backends := loadBackends()

	registry := backend.NewRegistry(backends)

	// 1. Initialize the Balancer with backends
	pool, err := balancer.NewServerPool(registry)
	if err != nil {
		log.Fatalf("Failed to initialize balancer: %v", err)
	}

	// 2. Build the HTTP handlers
	backendHandler := handlers.NewBackendHandler(registry)
	proxyHandler := proxy.NewHandler(pool)

	// 3. Compose the handlers onto one mux
	handler := handlers.NewRouter(backendHandler, proxyHandler)

	// 4. Start Listening
	log.Printf("Load balancer running on :8080 with %d baseline backend(s)", len(backends))
	log.Fatal(http.ListenAndServe(":8080", handler))
}
