package main

import (
	"encoding/json"
	"flag"
	"fmt"
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
	ptrID := flag.Int64("id", 1, "Instance ID of the server")
	flag.Parse()
	instanceID := *ptrID

	port := 2115 + 100 * (instanceID-1)

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

	log.Printf("Starting server %d on :%d...", instanceID, port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", port), nil))
}