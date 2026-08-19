package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
)

// [usage] go run scripts/run/main.go
func main() {
	// Root context cancelled on SIGINT/SIGTERM or when proxy/server fail
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	g, ctx := errgroup.WithContext(ctx)

	// 1. Start the Server
	g.Go(func() error {
		return runProcess(ctx, "Server", "go", "run", "scripts/simulate_server/main.go")
	})

	// 2. Start the Proxy
	g.Go(func() error {
		return runProcess(ctx, "Proxy", "go", "run", "cmd/proxy/main.go")
	})

	// Give the server and proxy a short delay to start up before launching traffic
	select {
	case <-time.After(3 * time.Second):
	case <-ctx.Done():
		return
	}

	// 3. Run 3 concurrent traffic simulation scripts
	var trafficWg sync.WaitGroup
	for i := 1; i <= 3; i++ {
		id := i
		trafficWg.Add(1)
		g.Go(func() error {
			defer trafficWg.Done()
			return runProcess(ctx, fmt.Sprintf("Traffic-%d", id), "go", "run", "scripts/simulate_traffic/main.go")
		})
	}

	log.Println("[Runner] All services started. Press Ctrl+C to stop.")

	if err := g.Wait(); err != nil && ctx.Err() == nil {
		log.Printf("[Runner] Error encountered: %v\n", err)
	}
}

func runProcess(ctx context.Context, prefix string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Printf("[%s] Starting...", prefix)
	err := cmd.Run()
	if err != nil && ctx.Err() == nil {
		return fmt.Errorf("[%s] exited with error: %w", prefix, err)
	}
	log.Printf("[%s] Exited", prefix)
	return nil
}