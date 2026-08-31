package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
)

var (
	count int = 0
	mu    sync.Mutex

	// healthy backs the /healthz endpoint. Flip it at runtime via /toggle-health
	// to watch the balancer evict and re-admit this instance.
	healthy atomic.Bool
)

func main() {
	// Define the command-line flag (defaulting to "instance-1" if omitted)
	ptrID := flag.Int64("id", 1, "Instance ID of the server")
	startHealthy := flag.Bool("healthy", true, "Whether /healthz reports 200 at startup")
	flag.Parse()
	instanceID := *ptrID
	healthy.Store(*startHealthy)

	port := 2115 + 100*(instanceID-1)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		currentCount := count
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"instance_id": instanceID,
			"visits":      currentCount,
		})
	})

	// Health endpoint probed by the balancer. Returns 200 when healthy, 503
	// otherwise.
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if healthy.Load() {
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, "ok")
			return
		}
		http.Error(w, "unhealthy", http.StatusServiceUnavailable)
	})

	// Flip health at runtime for manual testing.
	http.HandleFunc("/toggle-health", func(w http.ResponseWriter, r *http.Request) {
		now := !healthy.Load()
		healthy.Store(now)
		log.Printf("server %d health toggled: healthy=%v", instanceID, now)
		fmt.Fprintf(w, "healthy=%v\n", now)
	})

	log.Printf("Starting server %d on :%d...", instanceID, port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", port), nil))
}
