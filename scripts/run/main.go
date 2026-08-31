package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
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

	// Make sure nothing from a previous run is still squatting on the
	// proxy's port before we launch anything.
	freePort(8080)

	g, ctx := errgroup.WithContext(ctx)

	// 1. Start the Server
	for i := 1; i <= 3; i++ {
		id := i
		g.Go(func() error {
			return runProcess(ctx, "Server", "go", "run", "scripts/simulate_server/main.go", fmt.Sprintf("-id=%d", id))
		})
	}

	time.Sleep(3 * time.Second)

	// 2. Start the Proxy
	g.Go(func() error {
		return runProcess(ctx, "Proxy", "go", "run", "./cmd/balancer")
	})

	// Give the server and proxy a short delay to start up before launching traffic
	select {
	case <-time.After(3 * time.Second):
	case <-ctx.Done():
		return
	}

	// 3. Run 3 concurrent traffic simulation scripts
	var trafficWg sync.WaitGroup
	for i := 1; i <= 10; i++ {
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

// portPIDs returns the PIDs of any processes currently listening on the
// given TCP port, using whatever lookup tool is native to the OS.
func portPIDs(port int) []int {
	if runtime.GOOS == "windows" {
		// netstat's local-address column looks like "0.0.0.0:8080"; filter
		// to lines ending in exactly ":<port>" and take the trailing PID
		// column so we don't accidentally match a longer port number.
		cmd := fmt.Sprintf("netstat -ano | findstr :%d", port)
		out, err := exec.Command("cmd", "/C", cmd).Output()
		if err != nil {
			return nil
		}
		suffix := fmt.Sprintf(":%d", port)
		seen := map[int]bool{}
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 5 {
				continue
			}
			if !strings.HasSuffix(fields[1], suffix) {
				continue
			}
			if pid, err := strconv.Atoi(fields[len(fields)-1]); err == nil {
				seen[pid] = true
			}
		}
		pids := make([]int, 0, len(seen))
		for pid := range seen {
			pids = append(pids, pid)
		}
		return pids
	}

	// macOS / Linux: lsof lists PIDs directly, one per line.
	out, err := exec.Command("lsof", "-ti", fmt.Sprintf("tcp:%d", port)).Output()
	if err != nil {
		return nil
	}
	var pids []int
	for _, s := range strings.Fields(string(out)) {
		if pid, err := strconv.Atoi(s); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}

// freePort kills any process currently bound to the given port. If the
// lookup tool (lsof / netstat) isn't available, or nothing is listening,
// it's a no-op. Same approach as scripts/simulate_server/main.go, so a
// stale server or proxy from a previous run doesn't block this one.
func freePort(port int) {
	for _, pid := range portPIDs(port) {
		if pid == os.Getpid() {
			continue
		}
		log.Printf("[Runner] port %d in use by pid %d, killing it", port, pid)
		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		if err := proc.Kill(); err != nil {
			log.Printf("[Runner] failed to kill pid %d: %v", pid, err)
		}
	}
}
