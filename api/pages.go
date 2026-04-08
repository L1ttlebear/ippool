package api

import (
	"sort"
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

		activeHosts := make([]models.Host, 0, len(hosts))
		exhaustedHosts := make([]models.Host, 0, len(hosts))
		for _, h := range hosts {
			info, ok := trafficMap[h.ID]
			online := ok && info.Reachable
			if !online || h.State == models.StateFull || h.State == models.StateDead {
				exhaustedHosts = append(exhaustedHosts, h)
			} else {
				activeHosts = append(activeHosts, h)
			}
		}

		poolCarrierMap := make(map[string][]models.Host)
		for _, h := range hosts {
			if !h.IsLeader {
				continue
			}
			pool := h.Pool
			if pool == "" {
				pool = "default"
			}
			poolCarrierMap[pool] = append(poolCarrierMap[pool], h)
		}
		poolCards := make([]web.PoolCarrierCard, 0, len(poolCarrierMap))
		for pool, hs := range poolCarrierMap {
			poolCards = append(poolCards, web.PoolCarrierCard{Pool: pool, Hosts: hs})
		}
		sort.Slice(poolCards, func(i, j int) bool {
			return poolCards[i].Pool < poolCards[j].Pool
		})

		data := web.IndexPageData{
			Leader:          leader,
			Hosts:           hosts,
			ActiveHosts:     activeHosts,
			ExhaustedHosts:  exhaustedHosts,
			Domain:          domain,
			CircuitOpen:     cb.IsOpen(),
			NoHosts:         len(hosts) == 0,
			RecentLogs:      recentLogs,
			LastPoll:        time.Now(),
			CurrentLeaderID: leaderID,
			TrafficMap:      trafficMap,
			PoolCarriers:    poolCards,
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
	if len(hosts) == 0 {
		return m
	}

	hostIDs := make([]uint, 0, len(hosts))
	for _, h := range hosts {
		hostIDs = append(hostIDs, h.ID)
	}

	var hbs []models.HostHeartbeat
	if err := db.Where("host_id IN ?", hostIDs).Find(&hbs).Error; err != nil {
		return m
	}

	hbMap := make(map[uint]models.HostHeartbeat, len(hbs))
	for _, hb := range hbs {
		hbMap[hb.HostID] = hb
	}

	timeoutSecs, _ := config.GetAs[int](config.HeartbeatTimeoutSecondsKey, 90)
	if timeoutSecs <= 0 {
		timeoutSecs = 90
	}
	timeout := time.Duration(timeoutSecs) * time.Second

	for _, h := range hosts {
		hb, ok := hbMap[h.ID]
		if !ok || time.Since(hb.UpdatedAt) > timeout {
			m[h.ID] = web.HostTrafficInfo{
				HostID:       h.ID,
				Reachable:    false,
				SSHReachable: false,
				SSHError:     "heartbeat timeout",
			}
			continue
		}
		reachable := hb.NetworkOK && hb.SSHOK
		m[h.ID] = web.HostTrafficInfo{
			HostID:       h.ID,
			Reachable:    reachable,
			TrafficIn:    hb.TrafficIn,
			TrafficOut:   hb.TrafficOut,
			SSHReachable: hb.SSHOK,
			SSHError:     hb.Error,
			NetIface:     hb.NetIface,
		}
	}
	return m
}
