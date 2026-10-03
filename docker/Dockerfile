# syntax=docker/dockerfile:1

# ---- Build stage ----
FROM golang:1.26.6 AS build

WORKDIR /src

# Download dependencies first to leverage layer caching.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Build the statically linked binary.
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" -o /out/golancer ./cmd/balancer

# ---- Runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=build /out/golancer /app/golancer

# proxy / registry API / metrics
EXPOSE 8080 8081 2112

USER nonroot:nonroot

ENTRYPOINT ["/app/golancer"]
CMD []
