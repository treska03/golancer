package metrics

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Common holds the process-wide metrics shared across servers. It is created
// once and registered against the registry that /metrics serves.
type Common struct {
	requests *prometheus.CounterVec
}

// NewCommon creates the shared metrics and registers them with reg. It panics
// (via promauto) if a collector is registered twice, surfacing wiring mistakes
// at startup rather than silently dropping metrics.
func NewCommon(reg prometheus.Registerer) *Common {
	return &Common{
		requests: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests processed, labelled by path, method and status.",
		}, []string{"path", "method", "status"}),
	}
}

// ObserveRequest records a single processed request. Callers pass already
// extracted values, keeping this package independent of the HTTP layer.
func (c *Common) ObserveRequest(path, method string, status int) {
	c.requests.WithLabelValues(path, method, strconv.Itoa(status)).Inc()
}

func NewRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return reg
}
