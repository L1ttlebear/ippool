package api

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/L1ttlebear/ippool/config"
	"github.com/L1ttlebear/ippool/database/dbcore"
	"github.com/L1ttlebear/ippool/database/models"
	"github.com/L1ttlebear/ippool/engine"
	"github.com/L1ttlebear/ippool/web"
	"gorm.io/gorm"
)

// GetIndex renders the main monitoring page.
func GetIndex(cb *engine.CircuitBreaker) gin.HandlerFunc {
	return func(c *gin.Context) {
		db := dbcore.GetDBInstance()

		var hosts []models.Host
		db.Find(&hosts)

		var recentLogs []models.Log
		db.Order("id DESC").Limit(20).Find(&recentLogs)

		leaderID, _ := config.GetAs[uint](config.CurrentLeaderIDKey, uint(0))
		domain, _ := config.GetAs[string](config.CFRecordNameKey, "")

		var leader *models.Host
		for i := range hosts {
			if hosts[i].ID == leaderID {
				leader = &hosts[i]
				break
			}
		}

		trafficMap := loadLatestTrafficMap(db, hosts)

		data := web.IndexPageData{
			Leader:          leader,
			Hosts:           hosts,
			Domain:          domain,
			CircuitOpen:     cb.IsOpen(),
			RecentLogs:      recentLogs,
			LastPoll:        time.Now(),
			CurrentLeaderID: leaderID,
			TrafficMap:      trafficMap,
		}
		web.RenderIndex(c, data)
	}
}

// GetSettings renders the settings page.
func GetSettings(c *gin.Context) {
	db := dbcore.GetDBInstance()

	var hosts []models.Host
	db.Find(&hosts)

	cfg, _ := config.GetAll()
	if cfg == nil {
		cfg = map[string]any{}
	}

	web.RenderSettings(c, web.SettingsPageData{
		Hosts:  hosts,
		Config: cfg,
	})
}

func loadLatestTrafficMap(db *gorm.DB, hosts []models.Host) map[uint]web.HostTrafficInfo {
	m := make(map[uint]web.HostTrafficInfo, len(hosts))
	for _, h := range hosts {
		var rec models.CheckRecord
		if err := db.Where("host_id = ?", h.ID).Order("time DESC").First(&rec).Error; err != nil {
			continue
		}
		m[h.ID] = web.HostTrafficInfo{
			HostID:     h.ID,
			TrafficIn:  rec.TrafficIn,
			TrafficOut: rec.TrafficOut,
		}
	}
	return m
}
