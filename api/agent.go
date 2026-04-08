package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/L1ttlebear/ippool/config"
	"github.com/L1ttlebear/ippool/database/dbcore"
	"github.com/L1ttlebear/ippool/database/models"
	"gorm.io/gorm/clause"
)

type heartbeatRequest struct {
	HostID      uint   `json:"host_id"`
	HostName    string `json:"host_name"`
	NetworkOK   bool   `json:"network_ok"`
	SSHOK       bool   `json:"ssh_ok"`
	NetIface    string `json:"net_iface"`
	TrafficIn   int64  `json:"traffic_in"`
	TrafficOut  int64  `json:"traffic_out"`
	ProbeTarget string `json:"probe_target"`
	Error       string `json:"error"`
}

// AgentHeartbeat accepts host-agent heartbeat reports.
func AgentHeartbeat(c *gin.Context) {
	token := c.GetHeader("X-Agent-Token")
	expected, _ := config.GetAs[string](config.AgentSharedTokenKey, "")
	if expected == "" || token == "" || token != expected {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid agent token"})
		return
	}

	var req heartbeatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.HostID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "host_id required"})
		return
	}

	now := time.Now()
	hb := models.HostHeartbeat{
		HostID:      req.HostID,
		HostName:    req.HostName,
		AgentTime:   now,
		NetworkOK:   req.NetworkOK,
		SSHOK:       req.SSHOK,
		NetIface:    strings.TrimSpace(req.NetIface),
		TrafficIn:   req.TrafficIn,
		TrafficOut:  req.TrafficOut,
		ProbeTarget: req.ProbeTarget,
		Error:       strings.TrimSpace(req.Error),
		UpdatedAt:   now,
	}

	db := dbcore.GetDBInstance()
	if err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "host_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"host_name", "agent_time", "network_ok", "ssh_ok", "net_iface", "traffic_in", "traffic_out", "probe_target", "error", "updated_at"}),
	}).Create(&hb).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "heartbeat accepted"})
}
