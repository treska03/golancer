# Roadmap / TODO

Known gaps and planned work for golancer. See the main [README](README.md) for
what it does today. `[ X ]` = done, `[ ]` = open.

## Reliability

- [ X ] **Active health checking.** A prober GETs each backend's health path
  every interval. Backends are admitted and evicted using consecutive
  success/failure thresholds, and only healthy backends are selected.
- [ ] **Passive health checking / outlier ejection.** Connection failures
  and 5xx responses seen on real traffic don't affect a backend's health.
  Only the active prober can evict it, so a failing backend keeps getting
  traffic for up to `interval × unhealthy-threshold`.
- [ X ] **Retries / failover.** An unreachable backend is retried on another
  one, up to `balancing.max-retries` times. The request body is buffered so it
  can be replayed. `POST`/`PATCH` are never retried.
- [ ] **Retry on 5xx / retry budgets.** Retries only cover connection-level
  failures. There's no retry on 5xx responses and no global retry budget.
- [ X ] **Graceful shutdown.** `SIGINT`/`SIGTERM` drain every listener for up
  to 10s.

## Load balancing strategies

- [ X ] **Pluggable strategies.** `round-robin`, `random`,
  `least-connections` and `ip-hash`.
- [ X ] **Weights per backend.** Honored by `random`, `least-connections`
  and `ip-hash`.
- [ ] **Weighted round-robin.** `round-robin` ignores weights.
- [ ] **Round-robin fairness with backends down.** Unhealthy backends are
  skipped by walking forward from the counter, so the first healthy backend
  after a run of unhealthy ones gets an outsized share (`FIXME` in
  `internal/balancer/round_robin.go`).
- [ ] **Consistent hashing for `ip-hash`.** Plain modulo hashing remaps most
  clients whenever the healthy set changes.
- [ ] **Smarter failover for sticky routing.** `ip-hash` picks the same
  backend on every retry, so failover can't help until the prober evicts it.

## Configuration

- [ X ] **Configurable ports.** Each listener (proxy, registry, metrics) has
  its own port and read/write timeouts. Listeners bind all interfaces; the
  bind host isn't configurable.
- [ X ] **Configurable config path.** `-config` flag.
- [ X ] **Validation.** All config errors are collected and reported at
  startup, including port clashes between listeners.
- [ ] **Config hot-reload.** Optionally re-read the config on `SIGHUP`.

## Service discovery

- [ X ] **Kubernetes discovery.** `discovery.mode: kubernetes-discovery`
  watches a Service's EndpointSlices and mirrors its ready pods into the pool.
- [ ] **HTTPS to discovered pods.** Discovered backends always use `http://`.
- [ ] **Manual removal vs. discovery.** Deregistering a discovered backend
  through the API keeps it out of the pool until its pod leaves and rejoins
  the Service, because discovery still thinks it owns that address.

## Persistence

- [ ] **Persist runtime-registered backends.** Backends added through the API
  live only in memory and are lost on restart, leaving only the config
  baseline. Consider persisting the pool or a re-registration mechanism.

## Management API

- [ X ] **Separate listener and authentication.** The `/backends` API runs on
  its own port (`8081`) and requires a bearer token when
  `GOLANCER_REGISTRY_TOKEN` is set.
- [ ] **Wire the registry token into the Helm chart** (e.g. from a Secret).
  Today the chart deploys the API unauthenticated.
- [ X ] **Consistent status codes.** `register` returns `201 Created`.
- [ X ] **Health/readiness endpoints.** `/healthz` (liveness) and `/readyz`
  (200 while at least one backend is healthy, otherwise 503).
- [ ] **Expose weights in `GET /backends`.** The response includes
  `instanceID`, `url` and `healthy`, but not `weight`.

## Observability

- [ X ] **Prometheus metrics.** `http_requests_total{path,method,status}` on
  every listener, plus Go runtime and process metrics.
- [ ] **Per-backend metrics.** Request distribution, error rates and latency
  histograms per backend, a health gauge, and retry counts.
- [ ] **Request logging / tracing.** Proxied requests aren't logged, and
  neither is the chosen backend.
- [ ] **Health transition logging.** The prober doesn't log when a backend is
  admitted or evicted.
- [ ] **Structured logging.** The code mixes `log` and `log/slog`; move
  everything to `slog`.

## Proxying

- [ X ] **Server timeouts & body limit.** Per-listener read/write timeouts
  and `proxy.body-limit`.
- [ ] **Upstream timeouts & limits.** The proxy uses `http.DefaultTransport`:
  there's no response-header timeout to backends, and max header size isn't
  configurable.
- [ ] **Streaming request bodies.** Bodies are fully buffered (to allow
  retries), so uploads larger than `body-limit` aren't possible.
- [ X ] **Reverse-proxy error handling.** `ReverseProxy.ErrorHandler` triggers
  failover instead of returning a raw error.
- [ X ] **Trusted proxies.** `X-Forwarded-For`/`-Proto` are only believed
  from `proxy.trusted-proxies`.
- [ ] **TLS.** No HTTPS termination, and no TLS options for backends (custom
  CA, mTLS).

## Testing

- [ X ] **Unit tests.** Strategies (except round-robin), proxy
  failover/forwarding, handlers, config validation, middleware, Kubernetes
  watcher and discovery reconciliation.
- [ ] **Missing unit tests.** `Registry` (concurrent add/remove/list),
  the health prober's thresholds, round-robin rotation.
- [ ] **Integration tests.** Exercise the full binary (register → probe →
  proxy → deregister) against fake backends.

## Code cleanup

- [ ] Remove the stale `// ServerPool manages a list of backends.` comment in
  `internal/balancer/types.go`.
- [ ] Remove the unused `golancer.backends` block from
  `kubernetes/helm/values.yaml`. The balancer config comes from
  `configMaps.golancer-config`.
- [ ] Keep `config.yaml` and the chart's `configMaps.golancer-config` in sync,
  or generate one from the other.
