package cors

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}

func newTestRouter() *gin.Engine {
	r := gin.New()
	r.Use(Middleware())
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })
	return r
}

func TestMiddleware_NoOriginConfigured_AddsNoHeaders(t *testing.T) {
	t.Setenv("FRONTEND_ORIGIN", "")
	r := newTestRouter()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no Access-Control-Allow-Origin header, got %q", got)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestMiddleware_OriginConfigured_AddsHeadersOnRealRequest(t *testing.T) {
	t.Setenv("FRONTEND_ORIGIN", "https://d123.cloudfront.net")
	r := newTestRouter()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://d123.cloudfront.net" {
		t.Fatalf("expected Access-Control-Allow-Origin to be set, got %q", got)
	}
	if w.Code != http.StatusOK || w.Body.String() != "pong" {
		t.Fatalf("expected the real handler to still run, got %d %q", w.Code, w.Body.String())
	}
}

func TestMiddleware_PreflightRequest_ShortCircuitsWithNoContent(t *testing.T) {
	t.Setenv("FRONTEND_ORIGIN", "https://d123.cloudfront.net")
	r := newTestRouter()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodOptions, "/ping", nil))

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on OPTIONS preflight, got %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatalf("expected Access-Control-Allow-Methods to be set on preflight response")
	}
	if w.Body.Len() != 0 {
		t.Fatalf("expected the real handler not to run on a preflight request, got body %q", w.Body.String())
	}
}
