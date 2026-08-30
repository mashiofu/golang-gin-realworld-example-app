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

func TestMiddleware_NoOriginHeader_AddsNoCORSHeaders(t *testing.T) {
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

func TestMiddleware_ReflectsWhicheverOriginSentTheRequest(t *testing.T) {
	r := newTestRouter()

	for _, origin := range []string{"https://d123.cloudfront.net", "http://k8s-default-frontend-abc.us-east-1.elb.amazonaws.com", "http://localhost:4200"} {
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		req.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if got := w.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Fatalf("expected Access-Control-Allow-Origin to reflect %q, got %q", origin, got)
		}
		if got := w.Header().Get("Vary"); got != "Origin" {
			t.Fatalf("expected Vary: Origin so no downstream cache mixes responses across origins, got %q", got)
		}
	}
}

func TestMiddleware_PreflightRequest_ShortCircuitsWithNoContent(t *testing.T) {
	r := newTestRouter()

	req := httptest.NewRequest(http.MethodOptions, "/ping", nil)
	req.Header.Set("Origin", "https://d123.cloudfront.net")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

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
