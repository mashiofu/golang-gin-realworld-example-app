// Package cors adds the Access-Control-* headers browsers require before
// a page on one origin (the frontend) can read a response from another
// (this API, on its own ALB) - without them, the request can still
// succeed on the wire, but the browser blocks the frontend JS from ever
// seeing the response.
package cors

import "github.com/gin-gonic/gin"

// Middleware reflects the request's own Origin header back as
// Access-Control-Allow-Origin, rather than checking it against one
// fixed, configured value. Deliberate, not a shortcut: CORS's
// Access-Control-Allow-Origin exists to protect *credentialed* (cookie-
// based) sessions from being read cross-origin - this API authenticates
// with a bearer token in the Authorization header instead, which a
// cross-origin page can't silently attach the way a cookie would be, so
// a fixed origin allowlist buys little real security here. It does buy
// a real operational cost: the frontend now runs on its own
// EKS-managed ALB (see docs/design-decisions.md), a hostname Terraform
// never sees and that only exists after the frontend's own first
// deploy - a fixed FRONTEND_ORIGIN would mean backend and frontend each
// need the other's not-yet-known URL to deploy cleanly, a bootstrap
// order this sidesteps entirely.
//
// No Origin header at all (e.g. curl, server-to-server) means no CORS
// headers are added - harmless, since CORS is a browser-enforced
// concept only.
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Vary", "Origin")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}
