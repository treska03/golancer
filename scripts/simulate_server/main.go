package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
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

// server is a single simulated backend listening on one port. Each instance
// keeps its own visit counter and health flag so multiple backends spun up in
// the same process behave independently.
type server struct {
	instanceID int64
	port       int

	mu    sync.Mutex
	count int

	// healthy backs the /healthz endpoint. Flip it at runtime via /toggle-health
	// to watch the balancer evict and re-admit this instance.
	healthy atomic.Bool
}

// routes builds the HTTP handler for this backend. Each server gets its own mux
// so its counter, health flag and instance id stay independent of the others.
func (s *server) routes() http.Handler {
	mux := http.NewServeMux()

	// /bad_route always returns 404, for testing error handling. Registered
	// before "/" so it takes precedence over the catch-all handler.
	mux.HandleFunc("/bad_route", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// simulate request taking longer time
		time.Sleep(100 * time.Millisecond)

		s.mu.Lock()
		s.count++
		currentCount := s.count
		slog.Info(fmt.Sprintf("Request received: URL: %s", r.URL))
		s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"instance_id": s.instanceID,
			"port":        s.port,
			"visits":      currentCount,
		})
	})

	// Health endpoint probed by the balancer. Returns 200 when healthy, 503
	// otherwise.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if s.healthy.Load() {
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, "ok")
			return
		}
		http.Error(w, "unhealthy", http.StatusServiceUnavailable)
	})

	// Flip health at runtime for manual testing.
	mux.HandleFunc("/toggle-health", func(w http.ResponseWriter, r *http.Request) {
		now := !s.healthy.Load()
		s.healthy.Store(now)
		log.Printf("server %d (:%d) health toggled: healthy=%v", s.instanceID, s.port, now)
		fmt.Fprintf(w, "healthy=%v\n", now)
	})

	return mux
}

// serve claims the port (killing any stale holder) and blocks serving requests.
func (s *server) serve() error {
	freePort(s.port)
	addr := fmt.Sprintf("127.0.0.1:%d", s.port)
	ln := listenWhenFree(addr)
	log.Printf("Starting server %d on :%d...", s.instanceID, s.port)
	return http.Serve(ln, s.routes())
}

// parsePorts turns a comma-separated list like "2115,2215,2315" into port
// numbers, skipping blanks and rejecting anything that isn't a valid TCP port.
func parsePorts(s string) ([]int, error) {
	var ports []int
	for _, field := range strings.Split(s, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		p, err := strconv.Atoi(field)
		if err != nil || p < 1 || p > 65535 {
			return nil, fmt.Errorf("invalid port %q", field)
		}
		ports = append(ports, p)
	}
	return ports, nil
}

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
	ptrID := flag.Int64("id", 1, "Base instance ID (used when -ports is omitted, and as the starting id when it is)")
	portsFlag := flag.String("ports", "", "Comma-separated list of ports to serve on (e.g. 2115,2215). Overrides -id-derived port.")
	startHealthy := flag.Bool("healthy", true, "Whether /healthz reports 200 at startup")
	flag.Parse()

	// Determine which ports to serve. An explicit -ports list wins; otherwise
	// fall back to the single id-derived port for backward compatibility.
	var ports []int
	if *portsFlag != "" {
		var err error
		ports, err = parsePorts(*portsFlag)
		if err != nil {
			log.Fatalf("invalid -ports: %v", err)
		}
	}
	if len(ports) == 0 {
		ports = []int{int(2115 + 100*(*ptrID-1))}
	}

	// Spin up one backend per port, each in its own goroutine. Instance ids are
	// assigned sequentially from -id so every backend reports a distinct id.
	var wg sync.WaitGroup
	for i, port := range ports {
		s := &server{
			instanceID: *ptrID + int64(i),
			port:       port,
		}
		s.healthy.Store(*startHealthy)

		wg.Add(1)
		go func(s *server) {
			defer wg.Done()
			log.Fatal(s.serve())
		}(s)
	}
	wg.Wait()
}
