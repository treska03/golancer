# golancer

A small HTTP load balancer written in Go. It fronts a pool of interchangeable
backend instances of a **single service type** and spreads incoming requests
across them using round-robin. Backends can be added and removed **at runtime**
through a small management API — no restart required.

> Roadmap and known gaps live in [ROADMAP.md](ROADMAP.md).

## Features

- **Round-robin load balancing** across all registered backends.
- **Reverse proxying** of every incoming request to the selected backend.
- **Dynamic backend registration** via a JSON HTTP API (`/backends`).
- **Static seeding** of a baseline backend pool from `config.yaml` at startup.
- **Concurrency-safe registry** using a lock-free, copy-on-write read path.
- **Local dev harness** that spins up fake backends, the balancer, and traffic
  generators with a single command.

## How it works

```
                    ┌────────────────────────────────────────┐
  client request ─▶ │  golancer — listens on :8080           │
                    │                                        │
                    │  /                     → reverse proxy │ ─▶ backend (round-robin)
                    │  GET    /backends      → list          │
                    │  POST   /backends      → register      │
                    │  DELETE /backends/{id} → deregister    │
                    │  GET    /healthz       → liveness      │
                    │  GET    /readyz        → readiness     │
                    └────────────────────────────────────────┘
```

- The **catch-all route** (`/`) reverse-proxies each request. On every request
  the proxy asks the `ServerPool` for the next backend (round-robin via an
  atomic counter) and forwards the request there. If no backends are
  registered, it responds `503 Service Unavailable`.
- The **`/backends` API** manages the pool at runtime. It is served on the same
  listener as proxied traffic.

### Components

| Package | Responsibility |
| --- | --- |
| `cmd/balancer` | Entry point: loads config, wires everything together, listens on `:8080`. |
| `internal/domain` | The `Backend` type (instance ID + URL) and its JSON encoding. |
| `internal/backend` | `Registry` — concurrency-safe add / remove / list of backends (copy-on-write). |
| `internal/balancer` | `ServerPool` — round-robin selection over the registry's backends. |
| `internal/proxy` | Reverse-proxy HTTP handler that forwards to the selected backend. |
| `internal/handlers` | The `/backends` management API, the `/healthz` & `/readyz` probes, and the mux/router composition. |
| `scripts/` | Local dev helpers (fake backend, traffic generator, all-in-one runner). |

## Getting started

### Requirements

- Go 1.26+ (see [`go.mod`](go.mod)).

### Configuration

Baseline backends are read from [`config.yaml`](config.yaml) in the working
directory at startup:

```yaml
backends:
  - http://127.0.0.1:2115
  - http://127.0.0.1:2215
  - http://127.0.0.1:2315
```

Each URL must include a scheme and a host. Every backend is assigned a
generated instance ID at load time. Additional backends can be registered at
runtime via the API (see below).

### Run the balancer

```bash
go run ./cmd/balancer
```

The balancer listens on `:8080` and logs the number of baseline backends it
loaded.

### Try it end-to-end (dev harness)

The quickest way to see it working is the all-in-one runner, which starts three
fake backends (ports `2115`, `2215`, `2315`), the balancer, and three traffic
generators:

```bash
go run scripts/run/main.go
```

Press `Ctrl+C` to stop everything.

You can also run the pieces individually:

```bash
# a fake backend; -id=N listens on 2115 + 100*(N-1)
go run scripts/simulate_server/main.go -id=1

# send one request through the balancer and print the response
go run scripts/simulate_traffic/main.go
```

Each fake backend responds with its own `instance_id` and a running `visits`
count, so you can watch requests being distributed across the pool.

## Management API

All endpoints are served on `:8080` alongside proxied traffic.

### List backends

```bash
curl http://localhost:8080/backends
```

```json
{
  "backends": [
    { "instanceID": "…", "url": "http://127.0.0.1:2115" }
  ]
}
```

### Register a backend

```bash
curl -X POST http://localhost:8080/backends \
  -H 'Content-Type: application/json' \
  -d '{"url": "http://127.0.0.1:2415"}'
```

```json
{ "instanceID": "…" }
```

- `400` if the URL is missing/invalid (must include scheme and host).
- `409` if the URL is already registered.

### Deregister a backend

```bash
curl -X DELETE http://localhost:8080/backends/<instanceID>
```

- `200` on success.
- `404` if no backend has that instance ID.

## Health & readiness

The balancer exposes probes for its **own** process, distinct from the
per-backend health tracked by the registry.

### Liveness

```bash
curl http://localhost:8080/healthz
```

```json
{ "status": "ok" }
```

Always `200` while the process is serving — it does not inspect backends. A
running balancer with zero healthy backends is still alive and should be kept
out of rotation (see readiness), not restarted.

### Readiness

```bash
curl http://localhost:8080/readyz
```

```json
{ "status": "ready", "healthyBackends": 3 }
```

- `200` while at least one backend is healthy and therefore routable.
- `503` when no backend is healthy (`{"status":"unavailable","healthyBackends":0}`),
  so an upstream load balancer stops sending traffic to a balancer that can
  serve none.

## License

See [LICENSE](LICENSE).
