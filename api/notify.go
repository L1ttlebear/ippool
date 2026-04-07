package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/L1ttlebear/ippool/notifier"
)

// TestNotify sends a test notification to all configured channels.
func TestNotify(n *notifier.Notifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		n.Send("test", map[string]any{
			"message": "This is a test notification from IP Pool Monitor",
			"time":    time.Now().UTC().Format(time.RFC3339),
		})
		c.JSON(http.StatusOK, gin.H{"message": "test notification sent"})
	}
}
