package web

import (
	"embed"
	"html/template"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/L1ttlebear/ippool/database/models"
)

//go:embed templates/* static/*
var FS embed.FS

// IndexPageData is the data passed to the index template.
type IndexPageData struct {
	Leader          *models.Host
	Hosts           []models.Host
	Domain          string
	CircuitOpen     bool
	RecentLogs      []models.Log
	LastPoll        time.Time
	CurrentLeaderID uint
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

// newTmpl creates an isolated template set for a specific page.
// Each page gets its own set so {{define "content"}} blocks don't overwrite each other.
func newTmpl(page string) *template.Template {
	return template.Must(template.ParseFS(FS, "templates/base.html", "templates/"+page))
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
