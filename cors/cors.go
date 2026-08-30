// Package cors adds the Access-Control-* headers browsers require before
// a page on one origin (the frontend, served from CloudFront) can read a
// response from another (this API, served from an ALB) - without them,
// the request can still succeed on the wire, but the browser blocks the
// frontend JS from ever seeing the response.
package cors

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

// Middleware allows cross-origin requests from a single configured
// frontend origin (FRONTEND_ORIGIN, e.g. "https://d123.cloudfront.net" -
// no trailing slash). If unset, no CORS headers are added at all, same
// as before this middleware existed - safe default for local/test runs
// that don't set it.
func Middleware() gin.HandlerFunc {
	origin := os.Getenv("FRONTEND_ORIGIN")
	return func(c *gin.Context) {
		if origin == "" {
			c.Next()
			return
		}
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
