# Roadmap / TODO

Known gaps and planned work for golancer. These are things the current
implementation intentionally does **not** do yet. See the main
[README](README.md) for what it *does* do today.

## Reliability

- [ ] **Health checking.** The pool currently trusts that every registered
  backend is up. Round-robin will happily forward requests to a dead backend.
  Add active health checks (periodic probe) and/or passive health tracking
  (mark unhealthy on connection/5xx failures) and skip unhealthy backends in
  selection. The proxy already returns "no healthy backends registered" — the
  notion of "healthy" needs to actually exist.
- [ ] **Retries / failover.** If a chosen backend fails mid-request, try the
  next one instead of surfacing the error to the client.
- [ ] **Graceful shutdown.** `cmd/balancer` calls `http.ListenAndServe`
  directly and never shuts down cleanly. Use `http.Server` with signal
  handling and `Shutdown(ctx)` to drain in-flight requests.

## Load balancing strategies

- [ ] **Pluggable strategies.** Only round-robin exists (`ServerPool`). Add
  alternatives (least-connections, weighted round-robin, random, IP hash)
  behind the existing `Selector` interface.
- [ ] **Weights per backend.** Allow backends to carry a weight for
  weighted distribution.

## Configuration

- [ ] **Configurable listen address.** `:8080` is hard-coded in
  `cmd/balancer/main.go`. Make host/port configurable (config file / flag /
  env).
- [ ] **Configurable config path.** `config.yaml` is loaded from a fixed
  relative path in the working directory. Accept a `-config` flag.
- [ ] **Config hot-reload.** Optionally re-read `config.yaml` on `SIGHUP`.

## Persistence

- [ ] **Persist runtime-registered backends.** Backends added via the API live
  only in memory and are lost on restart, leaving only the `config.yaml`
  baseline. Consider persisting the live pool (file/store) or a
  re-registration mechanism.

## Management API

- [ ] **Authentication / authorization.** The `/backends` API is unauthenticated
  and shares the listener with proxied traffic. Anyone who can reach the proxy
  can rewrite the backend pool. Add auth and/or move management to a separate
  listener/port.
- [ ] **Consistent status codes.** `register` returns `200` where `201 Created`
  would be more accurate.
- [ ] **Health/readiness endpoints.** Add `/healthz` / `/readyz` for the
  balancer itself.

## Observability

- [ ] **Structured logging.** Currently uses the standard `log` package with
  ad-hoc messages. Move to structured logging (e.g. `log/slog`).
- [ ] **Metrics.** Expose request counts, per-backend distribution, error
  rates, latencies (e.g. Prometheus).
- [ ] **Request logging / tracing.** Log or trace proxied requests with the
  chosen backend.

## Proxying

- [ ] **Timeouts & limits.** No client/server timeouts, max header sizes, or
  body limits are configured — a slow backend or client can tie up resources.
- [ ] **TLS.** No HTTPS termination or backend TLS support.
- [ ] **Error handling in the reverse proxy.** Set `ReverseProxy.ErrorHandler`
  to control what clients see when a backend is unreachable (and to trigger
  failover — see Reliability).

## Testing

- [ ] **Unit tests.** No tests exist yet. Priorities: `Registry`
  (concurrent add/remove/list), `ServerPool` round-robin fairness and
  wrap-around, and the `/backends` handlers.
- [ ] **Integration tests.** Exercise the full path (register → proxy →
  deregister) against fake backends.

## Code cleanup

- [ ] Resolve the `TODO` in `internal/balancer/server_pool.go` about whether
  `GetServer` should return `*url.URL` or the full `*domain.Backend`.
- [ ] `NewServerPool` returns an `error` it never produces — either drop the
  return value or give it a real failure mode.
