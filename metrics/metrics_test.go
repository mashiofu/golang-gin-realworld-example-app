package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}

func newTestRouter() *gin.Engine {
	r := gin.New()
	r.Use(Middleware())
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })
	r.GET("/boom", func(c *gin.Context) { c.String(http.StatusInternalServerError, "boom") })
	return r
}

func scrape(t *testing.T, r *gin.Engine) string {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected /metrics to return 200, got %d", w.Code)
	}
	return w.Body.String()
}

func TestMiddleware_RecordsSuccessfulRequests(t *testing.T) {
	r := newTestRouter()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := scrape(t, r)
	if !strings.Contains(body, `http_requests_total{method="GET",route="/ping",status="200"}`) {
		t.Fatalf("expected a counter series for GET /ping status 200, got:\n%s", body)
	}
	if !strings.Contains(body, "http_request_duration_seconds_bucket") {
		t.Fatalf("expected latency histogram buckets to be present, got:\n%s", body)
	}
}

func TestMiddleware_RecordsErrorStatusCodes(t *testing.T) {
	r := newTestRouter()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}

	body := scrape(t, r)
	if !strings.Contains(body, `http_requests_total{method="GET",route="/boom",status="500"}`) {
		t.Fatalf("expected a counter series for GET /boom status 500, got:\n%s", body)
	}
}

func TestMiddleware_DoesNotInstrumentItself(t *testing.T) {
	r := newTestRouter()

	// Scrape twice - if /metrics were instrumenting itself, the second
	// scrape's body would contain a growing count for its own route.
	_ = scrape(t, r)
	body := scrape(t, r)

	if strings.Contains(body, `route="/metrics"`) {
		t.Fatalf("expected /metrics to be excluded from its own metrics, got:\n%s", body)
	}
}

func TestMiddleware_UsesRoutePatternNotRawPath(t *testing.T) {
	r := gin.New()
	r.Use(Middleware())
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))
	r.GET("/articles/:slug", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	for _, slug := range []string{"first-post", "second-post"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/articles/"+slug, nil))
	}

	body := scrape(t, r)
	if !strings.Contains(body, `route="/articles/:slug"`) {
		t.Fatalf("expected the route pattern, not a per-slug series, got:\n%s", body)
	}
	if strings.Contains(body, "first-post") || strings.Contains(body, "second-post") {
		t.Fatalf("expected no per-slug cardinality in metric labels, got:\n%s", body)
	}
}
