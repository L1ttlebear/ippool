package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/L1ttlebear/ippool/config"
	"github.com/L1ttlebear/ippool/database/dbcore"
	"github.com/L1ttlebear/ippool/database/models"
	"github.com/L1ttlebear/ippool/engine"
)
type agentInstallTask struct {
	ID        string   `json:"task_id"`
	Status    string   `json:"status"`
	Progress  int      `json:"progress"`
	Step      string   `json:"step"`
	Logs      []string `json:"logs"`
	HostID    uint     `json:"host_id"`
	HostName  string   `json:"host_name"`
	StartedAt int64    `json:"started_at"`
	UpdatedAt int64    `json:"updated_at"`
	Error     string   `json:"error,omitempty"`
}

var (
	agentInstallTasksMu sync.RWMutex
	agentInstallTasks   = map[string]*agentInstallTask{}
	stepProgressRegexp  = regexp.MustCompile(`\[(\d+)/(\d+)\]`)
)

func newAgentInstallTask(host models.Host) *agentInstallTask {
	id, _ := generateAgentSharedToken()
	now := time.Now().Unix()
	return &agentInstallTask{
		ID:        "task_" + id,
		Status:    "running",
		Progress:  5,
		Step:      "正在连接主机...",
		Logs:      []string{"开始安装 Agent..."},
		HostID:    host.ID,
		HostName:  host.Name,
		StartedAt: now,
		UpdatedAt: now,
	}
}

func setAgentInstallTask(task *agentInstallTask) {
	agentInstallTasksMu.Lock()
	defer agentInstallTasksMu.Unlock()
	agentInstallTasks[task.ID] = task
}

func getAgentInstallTask(id string) (*agentInstallTask, bool) {
	agentInstallTasksMu.RLock()
	defer agentInstallTasksMu.RUnlock()
	t, ok := agentInstallTasks[id]
	if !ok {
		return nil, false
	}
	cp := *t
	cp.Logs = append([]string(nil), t.Logs...)
	return &cp, true
}

func appendAgentInstallTaskLog(taskID, line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	p := parseInstallProgress(line)
	now := time.Now().Unix()
	agentInstallTasksMu.Lock()
	defer agentInstallTasksMu.Unlock()
	t, ok := agentInstallTasks[taskID]
	if !ok {
		return
	}
	t.Logs = append(t.Logs, line)
	if len(t.Logs) > 300 {
		t.Logs = t.Logs[len(t.Logs)-300:]
	}
	if p > 0 {
		t.Progress = p
		t.Step = line
	} else {
		t.Step = line
	}
	t.UpdatedAt = now
}

func finishAgentInstallTask(taskID string, ok bool, errMsg string) {
	now := time.Now().Unix()
	agentInstallTasksMu.Lock()
	defer agentInstallTasksMu.Unlock()
	t, exists := agentInstallTasks[taskID]
	if !exists {
		return
	}
	if ok {
		t.Status = "success"
		t.Progress = 100
		t.Step = "安装完成"
	} else {
		t.Status = "failed"
		if t.Progress < 5 {
			t.Progress = 5
		}
		t.Step = "安装失败"
		t.Error = errMsg
		if strings.TrimSpace(errMsg) != "" {
			t.Logs = append(t.Logs, "ERROR: "+errMsg)
		}
	}
	t.UpdatedAt = now
}

func parseInstallProgress(line string) int {
	m := stepProgressRegexp.FindStringSubmatch(line)
	if len(m) != 3 {
		return 0
	}
	cur, err1 := strconv.Atoi(m[1])
	total, err2 := strconv.Atoi(m[2])
	if err1 != nil || err2 != nil || total <= 0 {
		return 0
	}
	p := int(float64(cur) / float64(total) * 100.0)
	if p < 5 {
		p = 5
	}
	if p > 99 {
		p = 99
	}
	return p
}

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
	if !poolExists(host.Pool) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pool not found, please create it first"})
		return
	}
	if host.Priority <= 0 {
		next, err := getNextPriorityForPool(db, host.Pool)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		host.Priority = next
	}
	if err := db.Create(&host).Error; err != nil {
		if isUniqueConstraintError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "priority already exists in this pool"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if req.InstallAgent {
		token, err := ensureAgentSharedToken()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
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

		task := newAgentInstallTask(host)
		setAgentInstallTask(task)
		go runAgentInstallTask(task.ID, host, serverURL, token, interval)

		c.JSON(http.StatusCreated, gin.H{
			"host": host,
			"agent_install_task": gin.H{
				"task_id":   task.ID,
				"status":    task.Status,
				"progress":  task.Progress,
				"step":      task.Step,
				"started_at": task.StartedAt,
			},
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
	if strings.TrimSpace(updates.Pool) != "" && !poolExists(updates.Pool) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pool not found, please create it first"})
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

	token, err := ensureAgentSharedToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
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

	task := newAgentInstallTask(host)
	setAgentInstallTask(task)
	go runAgentInstallTask(task.ID, host, serverURL, token, interval)

	c.JSON(http.StatusOK, gin.H{
		"host": host,
		"agent_install_task": gin.H{
			"task_id":   task.ID,
			"status":    task.Status,
			"progress":  task.Progress,
			"step":      task.Step,
			"started_at": task.StartedAt,
		},
	})
}

func GetAgentInstallTask(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("task_id"))
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task id"})
		return
	}
	t, ok := getAgentInstallTask(taskID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	c.JSON(http.StatusOK, t)
}

func runAgentInstallTask(taskID string, host models.Host, serverURL, token string, interval int) {
	installer := &engine.AgentInstaller{}
	res := installer.InstallWithProgress(host, serverURL, token, interval, func(line string) {
		appendAgentInstallTaskLog(taskID, line)
	})
	if !res.Success {
		finishAgentInstallTask(taskID, false, fmt.Sprintf("%s\n%s", res.Error, strings.TrimSpace(res.Output)))
		return
	}
	finishAgentInstallTask(taskID, true, "")
}

func ensureAgentSharedToken() (string, error) {
	token, _ := config.GetAs[string](config.AgentSharedTokenKey, "")
	token = strings.TrimSpace(token)
	if token != "" {
		return token, nil
	}

	generated, err := generateAgentSharedToken()
	if err != nil {
		return "", err
	}
	if err := config.Set(config.AgentSharedTokenKey, generated); err != nil {
		return "", err
	}
	return generated, nil
}

func generateAgentSharedToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func poolExists(pool string) bool {
	p := strings.TrimSpace(pool)
	if p == "" {
		return false
	}
	gdb := dbcore.GetDBInstance()
	var cnt int64
	if err := gdb.Model(&models.Pool{}).Where("name = ?", p).Count(&cnt).Error; err != nil {
		return false
	}
	return cnt > 0
}

func getNextPriorityForPool(db any, pool string) (int, error) {
	gdb := dbcore.GetDBInstance()
	var maxPriority int
	if err := gdb.Model(&models.Host{}).Where("pool = ?", strings.TrimSpace(pool)).Select("COALESCE(MAX(priority), 0)").Scan(&maxPriority).Error; err != nil {
		return 0, err
	}
	return maxPriority + 1, nil
}
