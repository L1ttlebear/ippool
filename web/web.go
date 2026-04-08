package web

import (
	"embed"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/L1ttlebear/ippool/database/models"
)

//go:embed templates/* static/*
var FS embed.FS

// HostTrafficInfo holds traffic stats for a single host for display.
type HostTrafficInfo struct {
	HostID       uint
	Reachable    bool
	TrafficIn    int64 // bytes received (cumulative)
	TrafficOut   int64 // bytes sent (cumulative)
	SSHReachable bool
	SSHError     string
	NetIface     string
}

// IndexPageData is the data passed to the index template.
type IndexPageData struct {
	Leader          *models.Host
	Hosts           []models.Host
	ActiveHosts     []models.Host
	ExhaustedHosts  []models.Host
	Domain          string
	CircuitOpen     bool
	NoHosts         bool
	RecentLogs      []models.Log
	LastPoll        time.Time
	CurrentLeaderID uint
	// TrafficMap maps host ID -> latest traffic check result
	TrafficMap map[uint]HostTrafficInfo
}

// SettingsPageData is the data passed to the settings template.
type SettingsPageData struct {
	Hosts  []models.Host
	Config map[string]any
}

// LoginPageData is the data passed to the login template.
type LoginPageData struct {
	Error string
}

var eventTypeText = map[string]string{
	"state_change":  "状态变更",
	"leader_changed": "主机切换",
	"circuit_open":  "熔断触发",
	"circuit_close": "熔断恢复",
	"ddns_update":   "DDNS 更新",
	"ddns_match":    "DDNS 校验",
	"ddns_mismatch": "DDNS 异常",
	"exec":          "执行结果",
	"test":          "测试通知",
}

func eventTypeZh(t string) string {
	if v, ok := eventTypeText[t]; ok {
		return v
	}
	if t == "" {
		return "事件"
	}
	return t
}

func eventMessageZh(msgType, message string) string {
	m := strings.TrimSpace(message)
	if m == "" {
		return "-"
	}

	replacer := strings.NewReplacer(
		"leader changed", "主机切换",
		"circuit breaker opened", "熔断器已触发",
		"circuit breaker closed", "熔断器已恢复",
		"all hosts are Full or Dead", "所有主机均为满载或不可用",
		"pre-command failed", "前置命令执行失败",
		"no pre-command configured", "未配置前置命令",
		"DDNS updated", "DDNS 已更新",
		"DDNS update failed", "DDNS 更新失败",
	)
	m = replacer.Replace(m)

	if msgType == "state_change" {
		m = strings.ReplaceAll(m, "ready", "可用")
		m = strings.ReplaceAll(m, "full", "满载")
		m = strings.ReplaceAll(m, "dead", "不可用")
	}
	return m
}

// newTmpl creates an isolated template set for a specific page.
// Each page gets its own set so {{define "content"}} blocks don't overwrite each other.
func newTmpl(page string) *template.Template {
	funcs := template.FuncMap{
		"eventTypeZh":    eventTypeZh,
		"eventMessageZh": eventMessageZh,
	}
	return template.Must(template.New("base.html").Funcs(funcs).ParseFS(FS, "templates/base.html", "templates/"+page))
}

// RenderIndex renders the index page.
func RenderIndex(c *gin.Context, data IndexPageData) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	t := newTmpl("index.html")
	if err := t.ExecuteTemplate(c.Writer, "base.html", data); err != nil {
		c.String(http.StatusInternalServerError, "template error: %v", err)
	}
}

// RenderSettings renders the settings page.
func RenderSettings(c *gin.Context, data SettingsPageData) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	t := newTmpl("settings.html")
	if err := t.ExecuteTemplate(c.Writer, "base.html", data); err != nil {
		c.String(http.StatusInternalServerError, "template error: %v", err)
	}
}

// RenderLogin renders the login page.
func RenderLogin(c *gin.Context, data LoginPageData) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	t := newTmpl("login.html")
	if err := t.ExecuteTemplate(c.Writer, "base.html", data); err != nil {
		c.String(http.StatusInternalServerError, "template error: %v", err)
	}
}

// StaticHandler serves embedded static files.
func StaticHandler() gin.HandlerFunc {
	fileServer := http.FileServer(http.FS(FS))
	return func(c *gin.Context) {
		fileServer.ServeHTTP(c.Writer, c.Request)
	}
}

