package api

import (
	"net/http"
	"strconv"
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

type agentConfigResponse struct {
	HostID                uint   `json:"host_id"`
	HostName              string `json:"host_name"`
	HeartbeatURL          string `json:"heartbeat_url"`
	HeartbeatIntervalSecs int    `json:"heartbeat_interval_seconds"`
	ProbeTarget           string `json:"probe_target"`
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

// AgentConfig returns authoritative heartbeat parameters for a host agent.
func AgentConfig(c *gin.Context) {
	token := c.GetHeader("X-Agent-Token")
	expected, _ := config.GetAs[string](config.AgentSharedTokenKey, "")
	if expected == "" || token == "" || token != expected {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid agent token"})
		return
	}

	hostIDStr := strings.TrimSpace(c.Query("host_id"))
	if hostIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "host_id required"})
		return
	}
	hostID64, err := strconv.ParseUint(hostIDStr, 10, 64)
	if err != nil || hostID64 == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid host_id"})
		return
	}

	db := dbcore.GetDBInstance()
	var host models.Host
	if err := db.First(&host, uint(hostID64)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "host not found"})
		return
	}

	interval, _ := config.GetAs[int](config.AgentIntervalSecondsKey, 30)
	if interval < 5 {
		interval = 5
	}

	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	base := scheme + "://" + c.Request.Host

	c.JSON(http.StatusOK, agentConfigResponse{
		HostID:                host.ID,
		HostName:              host.Name,
		HeartbeatURL:          strings.TrimRight(base, "/") + "/api/agent/heartbeat",
		HeartbeatIntervalSecs: interval,
		ProbeTarget:           "https://www.hkt.com/",
	})
}
