package main

import (
	"log"

	"github.com/treska03/golancer/internal/backend"
	"github.com/treska03/golancer/internal/balancer"
	"github.com/treska03/golancer/internal/server"
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

	// 2. Build http server
	s := server.New(registry, pool)

	// 3. Start Listening
	log.Printf("Load balancer running on %s with %d baseline backend(s)", s.Addr, len(backends))
	log.Fatal(s.ListenAndServe())
}
