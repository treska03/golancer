package handlers

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/treska03/golancer/internal/server"
)

type PrometheusHandler struct {
	reg      *prometheus.Registry
	hTimeout time.Duration
}

func NewPrometheusHandler(reg *prometheus.Registry, cfg *server.Config) *PrometheusHandler {
	return &PrometheusHandler{reg: reg, hTimeout: cfg.WriteTimeout}
}

func (h *PrometheusHandler) Routes(mux *http.ServeMux) {
	mux.Handle("/metrics", promhttp.HandlerFor(h.reg, promhttp.HandlerOpts{Timeout: h.hTimeout}))
}
