package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"sync"
)

var (
	count int = 0
	mu    sync.Mutex
)

func main() {
	// Define the command-line flag (defaulting to "instance-1" if omitted)
	instanceID := flag.String("id", "instance-1", "Instance ID of the server")
	flag.Parse()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		currentCount := count
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"instance_id": *instanceID,
			"visits":      currentCount,
		})
	})

	log.Printf("Starting server %s on :2115...", *instanceID)
	log.Fatal(http.ListenAndServe("127.0.0.1:2115", nil))
}