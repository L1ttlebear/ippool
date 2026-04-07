package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/L1ttlebear/ippool/database/accounts"
)

// AuthMiddleware validates the session_token cookie.
// API routes return JSON 401; page routes redirect to /login.
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Cookie("session_token")
		if err != nil || cookie == "" {
			respondUnauthorized(c)
			return
		}

		uuid, err := accounts.GetSession(cookie)
		if err != nil {
			respondUnauthorized(c)
			return
		}

		c.Set("user_uuid", uuid)
		c.Next()
	}
}

func respondUnauthorized(c *gin.Context) {
	path := c.Request.URL.Path
	if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/ws") {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
	} else {
		c.Redirect(http.StatusFound, "/login")
		c.Abort()
	}
}
