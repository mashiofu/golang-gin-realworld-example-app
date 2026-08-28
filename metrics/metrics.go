// Package metrics exposes Prometheus metrics for the HTTP layer, scraped
// via the ServiceMonitor in conduit-platform's backend Helm chart (see
// helm/backend-chart/templates/servicemonitor.yaml there). Go runtime
// metrics (goroutines, GC, memory) come for free from
// promauto/client_golang's default registry - no extra code needed for
// those.
package metrics

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	requestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests processed, by method, route, and status code.",
		},
		[]string{"method", "route", "status"},
	)

	requestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency in seconds, by method, route, and status code.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "route", "status"},
	)
)

// Middleware records request count and latency for every request except
// /metrics itself (scraping the scrape endpoint is just noise). Uses
// c.FullPath() (the route pattern, e.g. "/api/articles/:slug") rather
// than the raw URL, so per-slug/per-username paths don't each become
// their own high-cardinality metric series.
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/metrics" {
			c.Next()
			return
		}

		start := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		status := strconv.Itoa(c.Writer.Status())

		requestsTotal.WithLabelValues(c.Request.Method, route, status).Inc()
		requestDuration.WithLabelValues(c.Request.Method, route, status).Observe(time.Since(start).Seconds())
	}
}
