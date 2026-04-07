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

var tmpl *template.Template

func init() {
	tmpl = template.Must(template.ParseFS(FS, "templates/*.html"))
}

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

// RenderIndex renders the index page.
func RenderIndex(c *gin.Context, data IndexPageData) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(c.Writer, "index.html", data); err != nil {
		c.String(http.StatusInternalServerError, "template error: %v", err)
	}
}

// RenderSettings renders the settings page.
func RenderSettings(c *gin.Context, data SettingsPageData) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(c.Writer, "settings.html", data); err != nil {
		c.String(http.StatusInternalServerError, "template error: %v", err)
	}
}

// RenderLogin renders the login page.
func RenderLogin(c *gin.Context, data LoginPageData) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(c.Writer, "login.html", data); err != nil {
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
