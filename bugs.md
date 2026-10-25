# Code review findings

Found by reading the code; none of these have been reproduced by running it.

| # | Severity | Category | Location | Issue | Failure scenario | Suggested fix |
|---|----------|----------|----------|-------|------------------|---------------|
| 9 | Medium | Correctness | [internal/proxy/proxy.go:120](internal/proxy/proxy.go#L120) | Proxy drops the backend's base path; the prober keeps it | A backend registered as `http://svc:9000/api` passes probes on `/api/healthz`, but traffic goes to `/users` instead of `/api/users` and 404s | Use `req.SetURL(target.URL)` or reject backend URLs with a path |
| 10 | Medium | Correctness | [internal/proxy/proxy.go:117](internal/proxy/proxy.go#L117) | Client IP lost behind proxies | `Rewrite` strips `X-Forwarded-*` and `SetXForwarded()` is never called, so backends lose the client IP and protocol. Behind an Ingress or HTTPRoute, ip-hash sends 100% of traffic to one backend | Call `req.SetXForwarded()`; support a list of trusted proxies for the ip-hash key |
| 15 | Low | Efficiency | [internal/proxy/proxy.go:115](internal/proxy/proxy.go#L115) | New ReverseProxy per attempt; `DefaultTransport` keeps 2 idle connections per host | Under concurrency most requests open a new TCP connection to the backend, adding latency and TIME_WAIT churn | Build one ReverseProxy per Handler with a dedicated `http.Transport` that raises `MaxIdleConnsPerHost` |
