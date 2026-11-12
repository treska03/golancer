# golancer

A small HTTP load balancer in Go. It spreads requests across interchangeable
backends of one service, health-checks them, and fails over when one is
unreachable.

## Quick start

```bash
go run scripts/run/main.go
```

This starts three fake backends, the balancer, and some test traffic. Press
`Ctrl+C` to stop. It kills anything already listening on ports 8080, 8081,
2115, 2215 and 2315.

To run just the balancer:

```bash
go run ./cmd/balancer -config config.yaml
```

By default it listens on three ports: `8080` for proxied traffic, `8081` for
the backend API and `2112` for metrics. These ports, and every other setting,
can be changed in [config.yaml](config.yaml), which has a comment on each one.

## Features

### Load balancing strategies

Set with `balancing.strategy` (default `round-robin`):

| Strategy | Picks | Uses weights |
| --- | --- | --- |
| `round-robin` | the next healthy backend in turn | no |
| `random` | a random backend, weighted | yes |
| `least-connections` | the fewest in-flight requests relative to weight | yes |
| `ip-hash` | the same backend for the same client IP | yes |

With `least-connections`, requests that arrive one at a time all tie at zero
connections and go to the first backend. It only spreads load under
concurrency.

### Backend sources

Backends can come from any of these at once:

- **Static list:** `backends` in `config.yaml`, each with an optional
  `weight`.
- **[Backend API](#backend-api):** add and remove backends at runtime.
- **[Kubernetes discovery](#kubernetes-discovery):** follow the pods of a
  Service.

### Health checks

- Each backend is probed with `GET /healthz` every interval. A `2xx` counts as
  a success.
- New backends get no traffic until a probe passes.
- A backend is dropped after 3 failures in a row and re-admitted after it
  passes again.

The path, interval, timeout and both thresholds are configurable under
`health`.

### Failover

If a backend can't be reached, the request is retried on another one, up to
`balancing.max-retries` times. `POST` and `PATCH` are never retried, and error
responses from a backend (4xx/5xx) are passed through as-is.

### Kubernetes discovery

golancer can watch a Service's EndpointSlices and balance across its pods
directly instead of going through the ClusterIP:

```yaml
discovery:
  mode: kubernetes-discovery
  kubernetes:
    service: my-backend # required
    port-name: http # Service port to use; omit for the unnamed port
```

- Ready pods are added (weight 1). Pods that become not-ready or go away are
  removed.
- Static and API-registered backends are left alone.
- Needs `get`/`list`/`watch` on `endpointslices`. The Helm chart grants this
  with `rbac.create: true`.
- To run outside a cluster, start `kubectl proxy` and set
  `kubernetes.api-server: http://127.0.0.1:8001` and `kubernetes.namespace`.

### Backend API

```bash
curl localhost:8081/backends
curl -X POST localhost:8081/backends -d '{"url": "http://127.0.0.1:2415", "weight": 2}'
curl -X DELETE localhost:8081/backends/<instanceID>
```

These list, add (returns `201` with the new `instanceID`) and remove backends.
Set `GOLANCER_REGISTRY_TOKEN` to require `Authorization: Bearer <token>`.
Backends added this way are lost on restart.

### Probes and metrics

- `GET /healthz` on the proxy port returns `200` while golancer is running.
- `GET /readyz` returns `503` when no backend is healthy, so whatever sits in
  front of golancer can take it out of rotation.
- `GET /metrics` on the metrics port serves Prometheus metrics:
  `http_requests_total{path,method,status}` plus Go runtime and process
  metrics.

### Other

- **Trusted proxies:** `X-Forwarded-For` is only trusted from addresses in
  `proxy.trusted-proxies`, so clients can't fake their IP for `ip-hash`.
- **Limits:** read/write timeouts per port, and a request body cap
  (`proxy.body-limit`, 16 KiB by default).
- **Graceful shutdown:** on `SIGINT`/`SIGTERM`, in-flight requests get up to
  10s to finish.

## More

- Kubernetes, Helm and Docker: [scripts/kubernetes/README.md](scripts/kubernetes/README.md)
- Known gaps: [ROADMAP.md](ROADMAP.md)
- Tests: `go test ./...`
