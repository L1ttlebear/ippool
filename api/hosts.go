package api

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/L1ttlebear/ippool/config"
	"github.com/L1ttlebear/ippool/database/dbcore"
	"github.com/L1ttlebear/ippool/database/models"
	"github.com/L1ttlebear/ippool/engine"
)
// GetHosts returns all hosts.
func GetHosts(c *gin.Context) {
	db := dbcore.GetDBInstance()
	var hosts []models.Host
	if err := db.Find(&hosts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, hosts)
}

// GetHost returns a single host by ID.
func GetHost(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	db := dbcore.GetDBInstance()
	var host models.Host
	if err := db.First(&host, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "host not found"})
		return
	}
	c.JSON(http.StatusOK, host)
}

// CreateHost creates a new host.
func CreateHost(c *gin.Context) {
	var req struct {
		models.Host
		InstallAgent        bool   `json:"install_agent"`
		AgentServerURL      string `json:"agent_server_url"`
		AgentIntervalSecond int    `json:"agent_interval_seconds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	host := req.Host
	if strings.TrimSpace(host.DisconnectCommand) == "" {
		tmpl, _ := config.GetAs[string](config.DefaultDisconnectCommandTemplateKey, "")
		host.DisconnectCommand = strings.TrimSpace(tmpl)
	}

	if net.ParseIP(host.IP) == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid IP address"})
		return
	}

	if host.Pool == "" {
		host.Pool = "default"
	}
	if host.SSHPort == 0 {
		host.SSHPort = 22
	}
	host.State = models.StateReady

	db := dbcore.GetDBInstance()
	if err := db.Create(&host).Error; err != nil {
		if isUniqueConstraintError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "priority already exists in this pool"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if req.InstallAgent {
		token, _ := config.GetAs[string](config.AgentSharedTokenKey, "")
		serverURL := strings.TrimSpace(req.AgentServerURL)
		if serverURL == "" {
			if c.Request.TLS != nil {
				serverURL = "https://" + c.Request.Host
			} else {
				serverURL = "http://" + c.Request.Host
			}
		}
		interval := req.AgentIntervalSecond
		if interval <= 0 {
			interval = 30
		}

		installer := &engine.AgentInstaller{}
		res := installer.Install(host, serverURL, token, interval)
		if !res.Success {
			c.JSON(http.StatusCreated, gin.H{
				"host":                    host,
				"agent_install_success":   false,
				"agent_install_error":     res.Error,
				"agent_install_output":    res.Output,
			})
			return
		}
		c.JSON(http.StatusCreated, gin.H{
			"host":                  host,
			"agent_install_success": true,
			"agent_install_output":  res.Output,
		})
		return
	}

	c.JSON(http.StatusCreated, host)
}

// UpdateHost updates an existing host.
func UpdateHost(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	db := dbcore.GetDBInstance()
	var host models.Host
	if err := db.First(&host, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "host not found"})
		return
	}

	var updates models.Host
	if err := c.ShouldBindJSON(&updates); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if updates.IP != "" && net.ParseIP(updates.IP) == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid IP address"})
		return
	}

	if err := db.Model(&host).Updates(&updates).Error; err != nil {
		if isUniqueConstraintError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "priority already exists in this pool"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, host)
}

// DeleteHost removes a host.
func DeleteHost(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	db := dbcore.GetDBInstance()
	if err := db.Delete(&models.Host{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

// SetHostState manually sets a host's state, bypassing the state machine.
func SetHostState(sm *engine.StateMachine) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseUint(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
			return
		}

		var body struct {
			State string `json:"state"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		state := models.HostState(body.State)
		if state != models.StateReady && state != models.StateFull && state != models.StateDead {
			c.JSON(http.StatusBadRequest, gin.H{"error": "state must be ready, full, or dead"})
			return
		}

		db := dbcore.GetDBInstance()
		if err := sm.ForceSet(db, uint(id), state); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "state updated"})
	}
}

// isUniqueConstraintError detects UNIQUE constraint violations from SQLite.
func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") || strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")
}

// CheckHostSSH manually tests SSH connectivity for a host.
func CheckHostSSH(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	db := dbcore.GetDBInstance()
	var host models.Host
	if err := db.First(&host, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "host not found"})
		return
	}

	checker := engine.NewHealthChecker(1, nil)
	result := checker.CheckHostSSH(host)

	status := "ok"
	if !result.SSHReachable {
		status = "failed"
	}

	c.JSON(http.StatusOK, gin.H{
		"host_id":       host.ID,
		"host_name":     host.Name,
		"ip":            host.IP,
		"ssh_reachable": result.SSHReachable,
		"ssh_error":     result.SSHError,
		"latency_ms":    result.LatencyMs,
		"checked_at":    time.Now().Format(time.RFC3339),
		"status":        status,
	})
}

// InstallHostAgent manually installs heartbeat agent for an existing host.
func InstallHostAgent(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	db := dbcore.GetDBInstance()
	var host models.Host
	if err := db.First(&host, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "host not found"})
		return
	}

	var req struct {
		AgentServerURL      string `json:"agent_server_url"`
		AgentIntervalSecond int    `json:"agent_interval_seconds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	token, _ := config.GetAs[string](config.AgentSharedTokenKey, "")
	serverURL := strings.TrimSpace(req.AgentServerURL)
	if serverURL == "" {
		if c.Request.TLS != nil {
			serverURL = "https://" + c.Request.Host
		} else {
			serverURL = "http://" + c.Request.Host
		}
	}
	interval := req.AgentIntervalSecond
	if interval <= 0 {
		interval = 30
	}

	installer := &engine.AgentInstaller{}
	res := installer.Install(host, serverURL, token, interval)
	if !res.Success {
		c.JSON(http.StatusOK, gin.H{
			"host":                 host,
			"agent_install_success": false,
			"agent_install_error":   res.Error,
			"agent_install_output":  res.Output,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"host":                  host,
		"agent_install_success": true,
		"agent_install_output":  res.Output,
	})
}
