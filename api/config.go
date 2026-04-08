package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/L1ttlebear/ippool/config"
)

// GetConfig returns all configuration values, masking sensitive fields.
func GetConfig(c *gin.Context) {
	cfg, err := config.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Mask sensitive fields - show only last 4 chars
	sensitiveKeys := []string{
		config.CFApiTokenKey,
		config.TelegramBotTokenKey,
	}
	for _, key := range sensitiveKeys {
		if val, ok := cfg[key]; ok {
			if s, ok := val.(string); ok && len(s) > 4 {
				cfg[key] = strings.Repeat("*", len(s)-4) + s[len(s)-4:]
			}
		}
	}

	if rulesVal, ok := cfg[config.DDNSPoolRulesKey]; ok {
		if arr, ok := rulesVal.([]any); ok {
			for i := range arr {
				m, ok := arr[i].(map[string]any)
				if !ok {
					continue
				}
				if token, ok := m["cf_api_token"].(string); ok && len(token) > 4 {
					m["cf_api_token"] = strings.Repeat("*", len(token)-4) + token[len(token)-4:]
				}
			}
			cfg[config.DDNSPoolRulesKey] = arr
		}
	}

	c.JSON(http.StatusOK, cfg)
}

// UpdateConfig batch-updates configuration values.
func UpdateConfig(c *gin.Context) {
	var updates map[string]any
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if v, ok := updates[config.PollIntervalKey]; ok {
		var interval float64
		switch val := v.(type) {
		case float64:
			interval = val
		case int:
			interval = float64(val)
		}
		if interval < 10 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "poll_interval must be at least 10 seconds"})
			return
		}
	}

	if err := config.SetMany(updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "config updated"})
}
