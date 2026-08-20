package main

import (
	"fmt"
	"golancer/m/internal/balancer"
	"golancer/m/internal/proxy"
	"log"
	"net/http"
)

func main() {
	// 1. Initialize the Balancer with backends
	pool, err := balancer.NewServerPool()
	if err != nil {
		fmt.Println("Hi")
		log.Fatalf("Failed to initialize balancer: %v", err)
	}

	// 2. Inject Balancer into Proxy
	proxyHandler := proxy.NewProxyHandler(pool)

	// 3. Start Listening
	log.Println("Load balancer running on :8080")
	log.Fatal(http.ListenAndServe(":8080", proxyHandler))
}