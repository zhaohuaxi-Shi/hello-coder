package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/hello-coder/hello-coder/internal/auth"
	"github.com/hello-coder/hello-coder/internal/response"
)

func RequireAuth(tm *auth.TokenManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader("Authorization")
		if raw == "" {
			response.Fail(c, http.StatusUnauthorized, 401, "unauthorized")
			c.Abort()
			return
		}
		const prefix = "Bearer "
		if !strings.HasPrefix(raw, prefix) {
			response.Fail(c, http.StatusUnauthorized, 401, "invalid authorization header")
			c.Abort()
			return
		}
		claims, err := tm.Parse(strings.TrimPrefix(raw, prefix))
		if err != nil {
			response.Fail(c, http.StatusUnauthorized, 401, "session expired")
			c.Abort()
			return
		}
		auth.SetUser(c, claims.UserID, claims.Username)
		c.Next()
	}
}

// BypassAuth injects a fixed local user (desktop mode, no login).
func BypassAuth(userID uint, username string) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth.SetUser(c, userID, username)
		c.Next()
	}
}
