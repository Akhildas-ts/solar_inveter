package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"solar_project/internal/api"
	"solar_project/internal/config"
	"solar_project/internal/service"
	"solar_project/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: .env file not found")
	}

	// Initialize logger
	logger.Init()
	logger.Info("Starting Solar Monitoring System")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}

	// Initialize database
	db, err := config.InitDatabase(cfg)
	if err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer db.Close()

	// Initialize services
	svc := service.NewService(db, cfg)
	defer svc.Close()

	// Setup HTTP server
	router := setupRouter(svc)
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.ServerPort),
		Handler: router,
	}

	// Start server in goroutine
	go func() {
		logger.Info(fmt.Sprintf("Server starting on port %d", cfg.ServerPort))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Server error:", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced shutdown:", err)
	}

	logger.Info("Server stopped gracefully")
}

func setupRouter(svc *service.Service) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	
	// Middleware
	r.Use(gin.Recovery())
	r.Use(api.Logger())
	r.Use(api.CORS())

	// Static files
	r.Static("/static", "./static")
	r.GET("/", func(c *gin.Context) {
		c.File("./static/index.html")
	})

	// API routes
	api.SetupRoutes(r, svc)

	return r
}