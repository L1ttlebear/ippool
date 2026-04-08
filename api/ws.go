package api

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/L1ttlebear/ippool/config"
	"github.com/L1ttlebear/ippool/database/dbcore"
	"github.com/L1ttlebear/ippool/database/models"
	"github.com/L1ttlebear/ippool/ws"
)

// WSHandler upgrades the connection to WebSocket, registers it with the hub,
// and immediately pushes a full snapshot.
func WSHandler(c *gin.Context) {
	conn, err := ws.UpgradeRequest(c, ws.CheckOrigin)
	if err != nil {
		slog.Warn("ws upgrade failed", "error", err)
		return
	}

	safe := ws.NewSafeConn(conn)
	ws.GlobalHub.Register(safe)
	defer ws.GlobalHub.Unregister(safe)

	sendSnapshot(safe)

	// Keep connection alive - read loop (discard messages, detect close)
	for {
		safe.SetReadDeadline(time.Now().Add(60 * time.Second))
		if _, _, err := safe.ReadMessage(); err != nil {
			break
		}
	}
}

func sendSnapshot(conn *ws.SafeConn) {
	db := dbcore.GetDBInstance()
	var hosts []models.Host
	db.Find(&hosts)

	leaderID, _ := config.GetAs[uint](config.CurrentLeaderIDKey, uint(0))
	circuitOpen := false
	for _, h := range hosts {
		if h.State == models.StateReady {
			circuitOpen = false
			break
		}
		circuitOpen = true
	}

	traffic := make(map[uint]map[string]int64, len(hosts))
	for _, h := range hosts {
		var rec models.CheckRecord
		if err := db.Where("host_id = ?", h.ID).Order("time DESC").First(&rec).Error; err != nil {
			continue
		}
		traffic[h.ID] = map[string]int64{
			"in":  rec.TrafficIn,
			"out": rec.TrafficOut,
		}
	}

	snapshot := map[string]any{
		"type": "snapshot",
		"data": map[string]any{
			"hosts":        hosts,
			"traffic":      traffic,
			"leader_id":    leaderID,
			"circuit_open": circuitOpen,
			"last_poll":    time.Now().UTC().Format(time.RFC3339),
		},
	}
	conn.WriteJSON(snapshot)
}
