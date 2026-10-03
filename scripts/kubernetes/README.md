# Local Kubernetes dev workflow

Helper scripts for running **golancer** on a throwaway local
[kind](https://kind.sigs.k8s.io/) cluster using the Helm chart in
[`kubernetes/helm`](../../kubernetes/helm). The loop is: build the images, load
them into the cluster (no registry required), and `helm upgrade --install`.

The pod runs two containers: golancer, plus a `dummy-backends` sidecar running
[`scripts/simulate_server`](../simulate_server) on `127.0.0.1:2115/2215/2315`.
Containers in a pod share a network namespace, so the loopback backends in
[`config.yaml`](../../config.yaml) work as-is and you get a self-contained load
balancer to experiment with.

## Prerequisites

| Tool      | Used for                          |
| --------- | --------------------------------- |
| `docker`  | building the image                |
| `kind`    | the local cluster                 |
| `helm`    | installing/upgrading the chart    |
| `kubectl` | talking to the cluster            |

```bash
brew install kind helm kubectl jq   # docker: Docker Desktop / colima
```

## TL;DR

```bash
# one-time: create the local cluster
scripts/kubernetes/prepare_cluster.sh

# whenever code, config, or the chart changes: rebuild + roll out
scripts/kubernetes/redeploy.sh
```

## Scripts

### `prepare_cluster.sh`

Creates the kind cluster (default name `golancer-local`) and switches your
`kubectl` context to it. Idempotent — running it again when the cluster exists
does nothing.

```bash
scripts/kubernetes/prepare_cluster.sh              # create if missing
scripts/kubernetes/prepare_cluster.sh --recreate   # delete then create fresh
scripts/kubernetes/prepare_cluster.sh --delete      # tear it down
```

### `redeploy.sh`

The everyday command. In one step it:

1. **builds** the golancer and `golancer-simulate` images via
   [`docker/build_image.sh`](../../docker/build_image.sh),
2. **loads** them into the kind cluster with `kind load docker-image` (so no
   container registry is involved), and
3. **`helm upgrade --install`s** the chart with the freshly built images.

Because step 3 always runs, `redeploy.sh` also applies any changes to the chart
templates or `values.yaml` — not just new Go code. Each run gets a **unique
image tag** (`dev-<git-sha>[-dirty]-<timestamp>`, shared by both images) so the
Deployment's pod template always changes and Kubernetes performs a real rollout.

```bash
scripts/kubernetes/redeploy.sh                  # build + load + upgrade
scripts/kubernetes/redeploy.sh --wait           # also block until Ready
scripts/kubernetes/redeploy.sh -t mytag         # pin the image tag
scripts/kubernetes/redeploy.sh --no-build -t X  # reuse already-built tags
scripts/kubernetes/redeploy.sh --no-simulate    # golancer only, no sidecar
```

### `docker/build_image.sh`

Standalone image builder (also called by `redeploy.sh`):

| Flag         | Dockerfile                   | Image               | Contents                                  |
| ------------ | ---------------------------- | ------------------- | ----------------------------------------- |
| (default)    | `docker/Dockerfile`          | `golancer`          | the balancer, with `config.yaml` baked in |
| `--simulate` | `docker/Dockerfile.simulate` | `golancer-simulate` | `simulate_server` fake backends           |

```bash
docker/build_image.sh                       # golancer:latest
docker/build_image.sh --simulate -t dev     # golancer-simulate:dev
docker/build_image.sh -n myrepo/golancer -t 1.2.3 --push
```

## Emulating the load balancer

golancer's `/readyz` returns **200 only while at least one backend is
healthy**. With the sidecar the pod becomes Ready after the first health probe
succeeds. With `--no-simulate` the loopback backends don't exist, so the pod
stays **NotReady** until you register a reachable backend via the API.

```bash
# terminal 1 - forward proxy, registry API, metrics and one simulated backend.
# (port-forward reaches loopback-bound ports inside the pod.)
kubectl -n golancer port-forward deploy/golancer 8080:8080 8081:8081 2112:2112 2115:2115

# terminal 2 - send traffic and count which backend served each request:
for i in $(seq 1 30); do curl -s localhost:8080/; echo; done | jq -r .port | sort | uniq -c

# inspect the pool and metrics:
curl -s localhost:8081/backends
curl -s localhost:2112/metrics | grep golancer

# fail a backend: it is evicted after 3 failed probes (~45s with a 15s interval),
# then re-run the traffic loop. Toggle again to bring it back.
curl -s localhost:2115/toggle-health
```

Each simulated backend responds with `{"instance_id", "port", "visits"}` after a
100ms delay. `/bad_route` always returns 404, for testing error handling.

> The default strategy in `config.yaml` is `least-connections`. Because every
> backend has the same latency, the 2/3/5 weights may not show up clearly in
> the counts. Switch to `round-robin` and redeploy to see weighting in action.

To change the simulated ports, set `simulate.ports` in
[`values.yaml`](../../kubernetes/helm/values.yaml) **and** update the backends
in `config.yaml` to match. The config is baked into the image, so the two must
be kept in sync by hand.

## Ports

golancer serves three listeners (see [`config.yaml`](../../config.yaml)). The Helm
`Service` only exposes the proxy; reach the others with `port-forward`.

| Port                  | Purpose                                     | In Service? |
| --------------------- | ------------------------------------------- | ----------- |
| `8080`                | reverse proxy + `/healthz` `/readyz`        | yes         |
| `8081`                | backend registration API (`/backends`)      | no          |
| `2112`                | Prometheus metrics (`/metrics`)             | no          |
| `2115`, `2215`, `2315`| simulated backends (sidecar, loopback only) | no          |

## Configuration

Every script reads these environment variables (defaults shown):

| Variable             | Default          | Meaning                       |
| -------------------- | ---------------- | ----------------------------- |
| `GOLANCER_CLUSTER`   | `golancer-local` | kind cluster name             |
| `GOLANCER_IMAGE`     | `golancer`       | image repository name         |
| `GOLANCER_RELEASE`   | `golancer`       | Helm release name             |
| `GOLANCER_NAMESPACE` | `golancer`       | target namespace (auto-created) |
| `GOLANCER_CHART_DIR` | `kubernetes/helm`| chart location                |

```bash
GOLANCER_NAMESPACE=lb scripts/kubernetes/redeploy.sh
```

## Troubleshooting

- **`kind cluster 'golancer-local' not found`** — run `prepare_cluster.sh` first.
- **Pod stuck `NotReady`** — no healthy backend. Check the sidecar with
  `kubectl -n golancer logs deploy/golancer -c dummy-backends`. With
  `--no-simulate`, register a backend (see above).
- **`ImagePullBackOff`** — an image wasn't loaded into kind. Re-run
  `redeploy.sh` (it runs `kind load` for both images), and note the chart uses
  `pullPolicy: IfNotPresent`.
- **Inspect a release**: `helm -n golancer status golancer` /
  `helm -n golancer get values golancer`.
- **Full reset**: `scripts/kubernetes/prepare_cluster.sh --recreate`.
