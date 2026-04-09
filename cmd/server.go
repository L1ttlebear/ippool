package cmd

import (
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
	"github.com/L1ttlebear/ippool/api"
	"github.com/L1ttlebear/ippool/cmd/flags"
	"github.com/L1ttlebear/ippool/config"
	"github.com/L1ttlebear/ippool/database/accounts"
	"github.com/L1ttlebear/ippool/database/auditlog"
	"github.com/L1ttlebear/ippool/database/dbcore"
	"github.com/L1ttlebear/ippool/engine"
	"github.com/L1ttlebear/ippool/notifier"
	logutil "github.com/L1ttlebear/ippool/utils/log"
	"github.com/L1ttlebear/ippool/web"
	"github.com/L1ttlebear/ippool/ws"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the IP Pool server",
	Run:   runServer,
}

func init() {
	rootCmd.AddCommand(serverCmd)
}

func runServer(_ *cobra.Command, _ []string) {
	// Ensure data directory exists
	if err := os.MkdirAll("./data", 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	// 1. Initialize database
	db := dbcore.GetDBInstance()

	// 2. Create default admin account if none exists
	var userCount int64
	db.Table("users").Count(&userCount)
	if userCount == 0 {
		username, passwd, err := accounts.CreateDefaultAdminAccount()
		if err != nil {
			log.Fatalf("Failed to create default admin account: %v", err)
		}
		slog.Info("Default admin account created", "username", username, "password", passwd)
	}

	// 3. Read concurrency config
	maxHealthConc, _ := config.GetAs[int](config.MaxHealthConcurrencyKey, 10)
	maxSSHConc, _ := config.GetAs[int](config.MaxSSHConcurrencyKey, 5)

	// 4. Initialize engine components
	sm := engine.NewStateMachine(ws.GlobalHub)
	hc := engine.NewHealthChecker(maxHealthConc, db)
	exec := engine.NewCommandExecutor(maxSSHConc)
	ddns := &engine.DDNSUpdater{}
	cb := &engine.CircuitBreaker{}

	// 5. Initialize Notifier from config
	telegramToken, _ := config.GetAs[string](config.TelegramBotTokenKey, "")
	telegramChatID, _ := config.GetAs[string](config.TelegramChatIDKey, "")
	webhookURL, _ := config.GetAs[string](config.WebhookURLKey, "")
	notifyTmpl, _ := config.GetAs[string](config.NotifyTemplateKey, "")
	n := notifier.New(telegramToken, telegramChatID, webhookURL, notifyTmpl)

	// Hot-reload notify template (no restart needed)
	config.Subscribe(func(e config.ConfigEvent) {
		if changed, tmpl := config.IsChangedT[string](e, config.NotifyTemplateKey); changed {
			n.SetTemplate(tmpl)
		}
	})

	// 6. Initialize and start Poller
	poller := engine.NewPoller(sm, hc, exec, ddns, cb, n, ws.GlobalHub)
	poller.Start(db)

	// 7. Start daily log cleanup goroutine
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			auditlog.RemoveOldLogs()
		}
	}()

	// 8. Setup Gin
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(logutil.GinLogger(), logutil.GinRecovery())

	// Static files (served from embedded FS)
	r.GET("/static/*filepath", func(c *gin.Context) {
		web.StaticHandler()(c)
	})

	// Public routes
	r.GET("/login", api.GetLogin)
	r.POST("/login", api.PostLogin)
	r.POST("/api/agent/heartbeat", api.AgentHeartbeat)

	// Authenticated routes
	auth := r.Group("/")
	auth.Use(api.AuthMiddleware())
	{
		// Pages
		auth.GET("/", api.GetIndex(cb))
		auth.GET("/settings", api.GetSettings)
		auth.GET("/appearance", api.GetAppearance)
		auth.GET("/logout", api.GetLogout)

		// Host API
		auth.GET("/api/hosts", api.GetHosts)
		auth.GET("/api/hosts/:id", api.GetHost)
		auth.POST("/api/hosts", api.CreateHost)
		auth.PUT("/api/hosts/:id", api.UpdateHost)
		auth.DELETE("/api/hosts/:id", api.DeleteHost)
		auth.PUT("/api/hosts/:id/state", api.SetHostState(sm))
		auth.POST("/api/hosts/:id/check-ssh", api.CheckHostSSH)
		auth.POST("/api/hosts/:id/install-agent", api.InstallHostAgent)

		// Config API
		auth.GET("/api/config", api.GetConfig)
		auth.PUT("/api/config", api.UpdateConfig)

		// Logs API
		auth.GET("/api/logs", api.GetLogs)
		auth.GET("/api/logs/recent", api.GetRecentLogs)

		// Notify API
		auth.POST("/api/notify/test", api.TestNotify(n))

		// DDNS API
		auth.POST("/api/ddns/verify", api.VerifyDDNS(ddns))

		// Account API
		auth.PUT("/api/account/password", api.ChangePassword)

		// WebSocket
		auth.GET("/ws", api.WSHandler)
	}

	// Listen address
	listen := getListen()
	slog.Info("Starting IP Pool server", "listen", listen)

	srv := &http.Server{
		Addr:    listen,
		Handler: r,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func getListen() string {
	listen := flags.Listen
	if !strings.Contains(listen, ":") {
		return ":" + listen
	}
	return listen
}
