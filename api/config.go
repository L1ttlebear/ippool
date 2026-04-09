package api

import (
	"fmt"
	"net/http"
	"net/url"
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

	if rulesRaw, ok := updates[config.DDNSPoolRulesKey]; ok {
		rules, err := normalizeDDNSRules(rulesRaw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		updates[config.DDNSPoolRulesKey] = rules
	}

	if v, ok := updates[config.SiteTitleKey]; ok {
		title := strings.TrimSpace(anyToString(v))
		if len(title) > 80 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "site_title too long (max 80)"})
			return
		}
		updates[config.SiteTitleKey] = title
	}

	if v, ok := updates[config.SiteLogoSVGKey]; ok {
		svg := strings.TrimSpace(anyToString(v))
		if len(svg) > 20000 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "site_logo_svg too long (max 20000 chars)"})
			return
		}
		updates[config.SiteLogoSVGKey] = svg
	}

	if v, ok := updates[config.BackgroundImageURLKey]; ok {
		raw := strings.TrimSpace(anyToString(v))
		if raw != "" {
			u, err := url.Parse(raw)
			if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "background_image_url must be a valid remote http/https URL"})
				return
			}
		}
		updates[config.BackgroundImageURLKey] = raw
	}

	if err := config.SetMany(updates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "config updated"})
}

func normalizeDDNSRules(raw any) ([]config.DdnsPoolRule, error) {
	arr, ok := raw.([]any)
	if !ok {
		// 兼容前端直接传 []map / []struct 的情况
		if typed, ok2 := raw.([]config.DdnsPoolRule); ok2 {
			arr = make([]any, 0, len(typed))
			for _, r := range typed {
				arr = append(arr, map[string]any{
					"pool":         r.Pool,
					"cf_api_token": r.CFApiToken,
					"cf_zone_id":   r.CFZoneID,
					"record_name":  r.RecordName,
					"enabled":      r.Enabled,
				})
			}
		} else {
			return nil, fmt.Errorf("ddns_pool_rules must be an array")
		}
	}

	seenPool := map[string]struct{}{}
	seenDomain := map[string]struct{}{}
	result := make([]config.DdnsPoolRule, 0, len(arr))
	for i, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("ddns_pool_rules[%d] must be an object", i)
		}

		pool := strings.TrimSpace(anyToString(m["pool"]))
		token := strings.TrimSpace(anyToString(m["cf_api_token"]))
		zone := strings.TrimSpace(anyToString(m["cf_zone_id"]))
		record := strings.TrimSpace(anyToString(m["record_name"]))
		enabled := anyToBool(m["enabled"], true)

		if pool == "" {
			return nil, fmt.Errorf("ddns_pool_rules[%d].pool is required", i)
		}
		if _, exists := seenPool[pool]; exists {
			return nil, fmt.Errorf("pool %q duplicated in ddns_pool_rules; one pool can only map to one domain", pool)
		}
		seenPool[pool] = struct{}{}

		if record != "" {
			if _, exists := seenDomain[record]; exists {
				return nil, fmt.Errorf("domain %q duplicated in ddns_pool_rules", record)
			}
			seenDomain[record] = struct{}{}
		}

		result = append(result, config.DdnsPoolRule{
			Pool:       pool,
			CFApiToken: token,
			CFZoneID:   zone,
			RecordName: record,
			Enabled:    enabled,
		})
	}
	return result, nil
}

func anyToString(v any) string {
	s, _ := v.(string)
	return s
}

func anyToBool(v any, def bool) bool {
	if v == nil {
		return def
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}
