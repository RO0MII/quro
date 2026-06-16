package main

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/quro/panel-api/internal/config"
	"github.com/quro/panel-api/internal/database"
	"github.com/quro/panel-api/internal/handlers"
	"github.com/quro/panel-api/internal/middleware"
	"github.com/quro/panel-api/internal/websocket"
)

func main() {
	cfg := config.Load()

	db, err := database.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := database.Migrate(db); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	if err := database.EnsureAdminUser(db, "admin", cfg.AdminEmail, cfg.AdminPassword); err != nil {
		log.Fatalf("failed to ensure admin user: %v", err)
	}
	log.Printf("admin user ready: %s", cfg.AdminEmail)

	hub := websocket.NewHub()
	go hub.Run()

	r := gin.Default()

	r.Use(middleware.CORS())

	api := r.Group("/api")
	{
		auth := api.Group("/auth")
		{
			auth.POST("/login", handlers.AutoLogin(db, cfg.JWTSecret))
		}

		protected := api.Group("")
		protected.Use(middleware.Auth(cfg.JWTSecret))
		{
			protected.GET("/servers", handlers.ListServers(db))
			protected.GET("/servers/:id", handlers.GetServer(db))
			protected.POST("/servers", handlers.CreateServer(db, hub))
			protected.DELETE("/servers/:id", handlers.DeleteServer(db))
			protected.POST("/servers/:id/start", handlers.StartServer(db))
			protected.POST("/servers/:id/stop", handlers.StopServer(db))
			protected.POST("/servers/:id/restart", handlers.RestartServer(db))
			protected.POST("/servers/:id/command", handlers.SendCommand(db))
			protected.GET("/servers/:id/metrics", handlers.GetMetrics(db))

			protected.GET("/servers/:id/files", handlers.ListFiles(db))
			protected.GET("/servers/:id/files/read", handlers.ReadFile(db))
			protected.POST("/servers/:id/files/write", handlers.WriteFile(db))
			protected.DELETE("/servers/:id/files/delete", handlers.DeleteFile(db))
			protected.POST("/servers/:id/files/rename", handlers.RenameFile(db))
			protected.POST("/servers/:id/files/folder", handlers.CreateFolder(db))
			protected.POST("/servers/:id/files/upload", handlers.UploadFile(db))

			protected.PATCH("/servers/:id/startup", handlers.UpdateServerStartup(db))

			protected.POST("/servers/:id/backups", handlers.CreateBackup(db))
			protected.GET("/servers/:id/backups", handlers.ListBackups(db))
			protected.POST("/servers/:id/backups/:backupId/restore", handlers.RestoreBackup(db))
			protected.DELETE("/servers/:id/backups/:backupId", handlers.DeleteBackup(db))

			protected.POST("/servers/:id/schedules", handlers.CreateSchedule(db))
			protected.GET("/servers/:id/schedules", handlers.ListSchedules(db))
			protected.PATCH("/servers/:id/schedules/:scheduleId", handlers.UpdateSchedule(db))
			protected.DELETE("/servers/:id/schedules/:scheduleId", handlers.DeleteSchedule(db))

			protected.GET("/nodes", handlers.ListNodes(db))
			protected.GET("/nodes/:id", handlers.GetNode(db))
			protected.POST("/nodes", handlers.CreateNode(db))
			protected.DELETE("/nodes/:id", handlers.DeleteNode(db))
		}

		// Daemon-facing endpoints: authenticated by X-Node-Token OR JWT
		daemon := api.Group("/nodes/:id")
		daemon.Use(middleware.NodeTokenAuth(db, cfg.JWTSecret))
		{
			daemon.GET("/config", handlers.GetNodeConfig(db))
			daemon.POST("/heartbeat", handlers.HandleHeartbeat(db))
		}
	}

	r.GET("/ws/console/:serverId", func(c *gin.Context) {
		serverId := c.Param("serverId")
		handlers.HandleConsoleWS(c, hub, serverId)
	})

	r.GET("/ws/metrics/:serverId", func(c *gin.Context) {
		serverId := c.Param("serverId")
		handlers.HandleMetricsWS(c, hub, serverId)
	})

	r.GET("/install.sh", func(c *gin.Context) {
		c.File("/opt/quro/public/install.sh")
	})

	r.GET("/install-daemon.sh", func(c *gin.Context) {
		c.File("/opt/quro/public/install-daemon.sh")
	})

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("panel API starting on :%s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}
