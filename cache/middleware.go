package cache

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// requestsTotal backs the cache-hit-rate dashboard panel/alert - hit rate
// is requestsTotal{result="hit"} / sum(requestsTotal). "disabled" covers
// the no-Redis-configured passthrough case (see redis.go), so a hit rate
// computed from this metric is never silently misleading about whether
// caching is even active.
var requestsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "cache_requests_total",
		Help: "Cache middleware outcomes, by result: hit, miss, bypass (authenticated request skipped the cache), or disabled (no Redis configured).",
	},
	[]string{"result"},
)

// responseRecorder tees the response body written by the downstream
// handler into a buffer (to store on a cache miss) while still writing
// straight through to the real client, unmodified.
type responseRecorder struct {
	gin.ResponseWriter
	body   *bytes.Buffer
	status int
}

func (w *responseRecorder) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *responseRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// IsAuthenticatedFunc reports whether the current request carries a
// recognized, authenticated user. It's injected by the caller so this
// package has no dependency on the users package's concrete types.
type IsAuthenticatedFunc func(c *gin.Context) bool

// Middleware returns a Gin middleware that caches 200-OK GET responses in
// Redis for ttl.
//
// requireAnonymous MUST be true for any endpoint whose response is
// personalized per-viewer - e.g. an article or profile payload includes
// `favorited`/`following` flags computed for whichever user is asking.
// Caching those globally by URL would leak one user's view (or writes) to
// a completely different user. When requireAnonymous is true, requests
// that carry a valid auth token skip the cache entirely and always hit
// the database, trading a bit of performance for a response that is
// always correct for that specific viewer.
//
// Endpoints with no per-viewer personalization at all (e.g. GET /api/tags)
// can pass requireAnonymous=false to benefit every caller, logged in or
// not.
//
// This is TTL-based only, with no explicit invalidation on writes - a
// deliberate simplification. Anonymous readers may see up to `ttl` of
// staleness after a create/update/favorite; authenticated readers (who
// bypass the cache under requireAnonymous) always see fresh data. That
// trade-off is called out here and in docs/design-decisions.md rather
// than hidden.
func Middleware(ttl time.Duration, requireAnonymous bool, isAuthenticated IsAuthenticatedFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !enabled || c.Request.Method != http.MethodGet {
			if c.Request.Method == http.MethodGet {
				requestsTotal.WithLabelValues("disabled").Inc()
			}
			c.Next()
			return
		}
		if requireAnonymous && isAuthenticated(c) {
			requestsTotal.WithLabelValues("bypass").Inc()
			c.Header("X-Cache", "BYPASS")
			c.Next()
			return
		}

		key := "cache:" + c.Request.URL.RequestURI()

		getCtx, cancel := context.WithTimeout(c.Request.Context(), 500*time.Millisecond)
		cached, err := client.Get(getCtx, key).Bytes()
		cancel()

		if err == nil {
			requestsTotal.WithLabelValues("hit").Inc()
			c.Header("X-Cache", "HIT")
			c.Data(http.StatusOK, "application/json; charset=utf-8", cached)
			c.Abort()
			return
		}

		requestsTotal.WithLabelValues("miss").Inc()
		c.Header("X-Cache", "MISS")
		rec := &responseRecorder{ResponseWriter: c.Writer, body: &bytes.Buffer{}, status: http.StatusOK}
		c.Writer = rec

		c.Next()

		if rec.status == http.StatusOK {
			// Best-effort set: the real response has already been sent to
			// the client above, so a slow/failed Redis write here must
			// never affect the request.
			setCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			_ = client.Set(setCtx, key, rec.body.Bytes(), ttl).Err()
		}
	}
}
