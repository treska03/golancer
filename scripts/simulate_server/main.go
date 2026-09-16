package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	count int = 0
	mu    sync.Mutex

	// healthy backs the /healthz endpoint. Flip it at runtime via /toggle-health
	// to watch the balancer evict and re-admit this instance.
	healthy atomic.Bool
)

// portPIDs returns the PIDs of any processes currently listening on the
// given TCP port, using whatever lookup tool is native to the OS.
func portPIDs(port int) []int {
	if runtime.GOOS == "windows" {
		// netstat's local-address column looks like "0.0.0.0:2115"; filter
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

// listenWhenFree binds to addr, retrying for a bit if the port is still
// held. Killing a process (especially on Windows, via TerminateProcess) is
// asynchronous — the OS can take a moment to actually release the socket
// after the kill call returns, so a bind attempted immediately afterward
// can still fail with "address already in use".
func listenWhenFree(addr string) net.Listener {
	var lastErr error
	for i := 0; i < 50; i++ {
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			return ln
		}
		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}
	log.Fatalf("could not bind to %s after retries: %v", addr, lastErr)
	return nil
}

// freePort kills any process currently bound to the given port so this
// instance can reliably claim it. If the lookup tool (lsof / netstat) isn't
// available, or nothing is listening, it's a no-op.
func freePort(port int) {
	for _, pid := range portPIDs(port) {
		if pid == os.Getpid() {
			continue
		}
		log.Printf("port %d in use by pid %d, killing it", port, pid)
		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		// os.Process.Kill() maps to SIGKILL on Unix and TerminateProcess on
		// Windows, so this one call works on both.
		if err := proc.Kill(); err != nil {
			log.Printf("failed to kill pid %d: %v", pid, err)
		}
	}
}

func main() {
	// Define the command-line flag (defaulting to "instance-1" if omitted)
	ptrID := flag.Int64("id", 1, "Instance ID of the server")
	startHealthy := flag.Bool("healthy", true, "Whether /healthz reports 200 at startup")
	flag.Parse()
	instanceID := *ptrID
	healthy.Store(*startHealthy)

	port := int(2115 + 100*(instanceID-1))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// simulate request taking longer time
		time.Sleep(100 * time.Millisecond)

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

	freePort(port)

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln := listenWhenFree(addr)

	log.Printf("Starting server %d on :%d...", instanceID, port)
	log.Fatal(http.Serve(ln, nil))
}
