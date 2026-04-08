package api

import (
	"net/http"
	"strings"

	"github.com/L1ttlebear/ippool/config"
	"github.com/L1ttlebear/ippool/database/dbcore"
	"github.com/L1ttlebear/ippool/database/models"
	"github.com/L1ttlebear/ippool/engine"
	"github.com/gin-gonic/gin"
)

// VerifyDDNS resolves configured DDNS domain and verifies it matches the current leader IP.
func VerifyDDNS(ddns *engine.DDNSUpdater) gin.HandlerFunc {
	return func(c *gin.Context) {
		leaderID, _ := config.GetAs[uint](config.CurrentLeaderIDKey, uint(0))
		if leaderID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no current leader, cannot verify DDNS"})
			return
		}

		var leader models.Host
		db := dbcore.GetDBInstance()
		if err := db.First(&leader, "id = ?", leaderID).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "current leader not found"})
			return
		}

		recordName := ""
		rules, _ := config.GetAs[[]config.DdnsPoolRule](config.DDNSPoolRulesKey, []config.DdnsPoolRule{})
		for _, rule := range rules {
			if !rule.Enabled {
				continue
			}
			if strings.TrimSpace(rule.Pool) != strings.TrimSpace(leader.Pool) {
				continue
			}
			recordName = strings.TrimSpace(rule.RecordName)
			if recordName != "" {
				break
			}
		}
		if recordName == "" {
			recordName, _ = config.GetAs[string](config.CFRecordNameKey, "")
			recordName = strings.TrimSpace(recordName)
		}
		if recordName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no DDNS record configured for current leader pool"})
			return
		}

		matched, resolved, err := ddns.VerifyResolvedIP(recordName, leader.IP)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"ok":             false,
				"domain":         recordName,
				"expected_ip":    leader.IP,
				"leader_id":      leader.ID,
				"leader_name":    leader.Name,
				"leader_state":   leader.State,
				"leader_pool":    leader.Pool,
				"resolved_ips":   resolved,
				"error":          err.Error(),
				"status_message": "DDNS 检测失败：域名解析异常",
			})
			return
		}

		statusMessage := "DDNS 检测通过：域名解析与当前 Leader IP 一致"
		if !matched {
			statusMessage = "DDNS 检测失败：域名返回 IP 与当前 Leader 不匹配"
		}

		c.JSON(http.StatusOK, gin.H{
			"ok":             matched,
			"domain":         recordName,
			"expected_ip":    leader.IP,
			"leader_id":      leader.ID,
			"leader_name":    leader.Name,
			"leader_state":   leader.State,
			"leader_pool":    leader.Pool,
			"resolved_ips":   resolved,
			"status_message": statusMessage,
		})
	}
}
