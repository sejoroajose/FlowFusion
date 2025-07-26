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

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"go.uber.org/zap"

	"flowfusion/bridge-orchestrator/internal/api"
	"flowfusion/bridge-orchestrator/internal/config"
	"flowfusion/bridge-orchestrator/internal/database"
	"flowfusion/bridge-orchestrator/pkg/adapters"
	"flowfusion/bridge-orchestrator/pkg/orchestrator"
	"flowfusion/bridge-orchestrator/pkg/twap"
)

func main() {
	// Load environment variables
	if err := godotenv.Load("../../.env"); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	// Initialize logger
	logger, err := initLogger()
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync()

	logger.Info("🌊 Starting FlowFusion Bridge Orchestrator")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		logger.Fatal("Failed to load configuration", zap.Error(err))
	}

	logger.Info("Configuration loaded",
		zap.String("environment", cfg.Environment),
		zap.Int("port", cfg.Port),
		zap.Strings("supported_chains", cfg.SupportedChains))

	// Initialize database
	db, err := database.Initialize(cfg.DatabaseURL)
	if err != nil {
		logger.Fatal("Failed to initialize database", zap.Error(err))
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("Error closing database", zap.Error(err))
		}
	}()

	logger.Info("Database connected successfully")

	// Initialize chain adapters
	adapterManager, err := adapters.NewManager(cfg, logger)
	if err != nil {
		logger.Fatal("Failed to initialize chain adapters", zap.Error(err))
	}

	logger.Info("Chain adapters initialized",
		zap.Int("adapter_count", adapterManager.GetAdapterCount()))

	// Initialize TWAP engine
	twapEngine, err := twap.NewEngine(cfg.TWAPConfig, db, adapterManager, logger)
	if err != nil {
		logger.Fatal("Failed to initialize TWAP engine", zap.Error(err))
	}

	logger.Info("TWAP engine initialized")

	// Initialize orchestrator
	orch, err := orchestrator.New(cfg, db, adapterManager, twapEngine, logger)
	if err != nil {
		logger.Fatal("Failed to initialize orchestrator", zap.Error(err))
	}

	logger.Info("Bridge orchestrator initialized")

	// Start background services
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start orchestrator
	go func() {
		logger.Info("Starting bridge orchestrator...")
		if err := orch.Start(ctx); err != nil {
			logger.Error("Orchestrator failed", zap.Error(err))
		}
	}()

	// Start TWAP engine
	go func() {
		logger.Info("Starting TWAP engine...")
		if err := twapEngine.Start(ctx); err != nil {
			logger.Error("TWAP engine failed", zap.Error(err))
		}
	}()

	// Initialize HTTP server
	server := setupHTTPServer(cfg, orch, twapEngine, db, logger)

	// Start HTTP server
	go func() {
		logger.Info("Starting HTTP server", zap.Int("port", cfg.Port))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Failed to start HTTP server", zap.Error(err))
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("Shutting down gracefully...")

	// Cancel context to stop background services
	cancel()

	// Shutdown HTTP server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("Failed to shutdown HTTP server gracefully", zap.Error(err))
	}

	logger.Info("FlowFusion Bridge Orchestrator stopped")
}

func initLogger() (*zap.Logger, error) {
	env := os.Getenv("NODE_ENV")
	if env == "production" {
		return zap.NewProduction()
	}
	return zap.NewDevelopment()
}

func setupHTTPServer(
	cfg *config.Config,
	orch *orchestrator.Orchestrator,
	twapEngine *twap.Engine,
	db database.DB,
	logger *zap.Logger,
) *http.Server {
	// Set Gin mode
	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// Create router
	router := gin.New()
	
	// Add middleware
	router.Use(gin.Recovery())
	router.Use(api.LoggerMiddleware(logger))
	router.Use(api.CORSMiddleware())

	// Setup routes
	api.SetupRoutes(router, orch, twapEngine, db, logger)

	// Create server
	return &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
}