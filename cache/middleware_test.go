package cache

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// metricsSnapshot scrapes the current process's default Prometheus
// registry - good enough to assert "this counter series exists and
// increased," without needing a fresh registry per test.
func metricsSnapshot(t *testing.T) string {
	t.Helper()
	r := gin.New()
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return w.Body.String()
}

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

// TestMiddleware_DisabledIsPassthrough covers the default, no-Redis-configured
// state (e.g. plain `go test ./...` on a laptop with nothing running) -
// every request must reach the real handler, unmodified.
func TestMiddleware_DisabledIsPassthrough(t *testing.T) {
	enabled = false // force disabled regardless of environment/other tests

	hits := 0
	r := gin.New()
	never := func(c *gin.Context) bool { return false }
	r.GET("/anon-only", Middleware(time.Minute, true, never), func(c *gin.Context) {
		hits++
		c.JSON(http.StatusOK, gin.H{"hits": hits})
	})

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/anon-only", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	}
	if hits != 3 {
		t.Fatalf("handler should run on every request while caching is disabled, got %d calls", hits)
	}
}

// The remaining tests need a real Redis (docker-compose provides one, as
// does CI). They skip cleanly when REDIS_ADDR isn't set rather than
// failing, so the suite stays hermetic for anyone running `go test ./...`
// with no services running.
func requireRedis(t *testing.T) {
	t.Helper()
	if os.Getenv("REDIS_ADDR") == "" {
		t.Skip("REDIS_ADDR not set; skipping test that requires a real redis (see docker-compose.yml)")
	}
	Init()
	if !Enabled() {
		t.Fatal("REDIS_ADDR is set but cache did not become enabled - is redis reachable?")
	}
}

// TestMiddleware_BypassesCacheForAuthenticatedRequests is the correctness
// property this whole package exists to protect: an authenticated caller
// must never receive another user's cached, personalized response.
func TestMiddleware_BypassesCacheForAuthenticatedRequests(t *testing.T) {
	requireRedis(t)

	hits := 0
	r := gin.New()
	alwaysAuthed := func(c *gin.Context) bool { return true }
	r.GET("/personalized", Middleware(time.Minute, true, alwaysAuthed), func(c *gin.Context) {
		hits++
		c.JSON(http.StatusOK, gin.H{"hits": hits})
	})

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/personalized", nil))
		if got := w.Header().Get("X-Cache"); got != "BYPASS" {
			t.Fatalf("expected X-Cache: BYPASS for an authenticated request, got %q", got)
		}
	}
	if hits != 3 {
		t.Fatalf("authenticated requests must always reach the handler, got %d calls, want 3", hits)
	}
}

// TestMiddleware_CachesAnonymousGETs proves the actual miss-then-hit path
// against a real Redis: same handler, second call served from cache.
func TestMiddleware_CachesAnonymousGETs(t *testing.T) {
	requireRedis(t)

	hits := 0
	r := gin.New()
	neverAuthed := func(c *gin.Context) bool { return false }
	r.GET("/cacheable/:id", Middleware(time.Minute, true, neverAuthed), func(c *gin.Context) {
		hits++
		c.JSON(http.StatusOK, gin.H{"id": c.Param("id"), "hits": hits})
	})

	path := "/cacheable/test-key-" + time.Now().Format("150405.000000000")

	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, path, nil))
	if got := w1.Header().Get("X-Cache"); got != "MISS" {
		t.Fatalf("expected first request to MISS, got %q", got)
	}

	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, path, nil))
	if got := w2.Header().Get("X-Cache"); got != "HIT" {
		t.Fatalf("expected second request to HIT, got %q", got)
	}

	if w1.Body.String() != w2.Body.String() {
		t.Fatalf("cached body must match the original response: %q vs %q", w1.Body.String(), w2.Body.String())
	}
	if hits != 1 {
		t.Fatalf("handler should only run once, on the miss; ran %d times", hits)
	}
}

// TestMiddleware_TagsAreCachedForEveryone covers the requireAnonymous=false
// path used for /api/tags, which has no per-viewer personalization at all.
func TestMiddleware_TagsAreCachedForEveryone(t *testing.T) {
	requireRedis(t)

	hits := 0
	r := gin.New()
	alwaysAuthed := func(c *gin.Context) bool { return true }
	r.GET("/tags-like/:id", Middleware(time.Minute, false, alwaysAuthed), func(c *gin.Context) {
		hits++
		c.JSON(http.StatusOK, gin.H{"hits": hits})
	})

	path := "/tags-like/test-key-" + time.Now().Format("150405.000000000")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, path, nil))
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, path, nil))

	if got := w2.Header().Get("X-Cache"); got != "HIT" {
		t.Fatalf("expected a cache HIT even for an 'authenticated' caller when requireAnonymous=false, got %q", got)
	}
	if hits != 1 {
		t.Fatalf("handler should only run once, got %d calls", hits)
	}
}

// TestMiddleware_RecordsCacheRequestsTotal exercises all four outcomes
// and confirms cache_requests_total (the metric the cache-hit-rate
// dashboard panel/alert reads) actually has a series for each - this is
// what backs "is caching even doing anything" in Grafana, so it matters
// that it's real, not just that X-Cache renders correctly.
func TestMiddleware_RecordsCacheRequestsTotal(t *testing.T) {
	// "disabled" doesn't need Redis - covers the no-Redis-configured
	// passthrough on its own.
	enabled = false
	r := gin.New()
	never := func(c *gin.Context) bool { return false }
	r.GET("/metrics-disabled-probe", Middleware(time.Minute, true, never), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/metrics-disabled-probe", nil))

	requireRedis(t) // re-enables the client for the rest of this test

	r2 := gin.New()
	alwaysAuthed := func(c *gin.Context) bool { return true }
	r2.GET("/metrics-bypass-probe", Middleware(time.Minute, true, alwaysAuthed), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})
	r2.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/metrics-bypass-probe", nil))

	r3 := gin.New()
	neverAuthed := func(c *gin.Context) bool { return false }
	path := "/metrics-hitmiss-probe-" + time.Now().Format("150405.000000000")
	r3.GET(path, Middleware(time.Minute, true, neverAuthed), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{})
	})
	r3.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil)) // miss
	r3.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil)) // hit

	body := metricsSnapshot(t)
	for _, result := range []string{"disabled", "bypass", "miss", "hit"} {
		want := `cache_requests_total{result="` + result + `"}`
		if !strings.Contains(body, want) {
			t.Errorf("expected a %s series in cache_requests_total, got:\n%s", result, body)
		}
	}
}
