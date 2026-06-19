package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/contrib/v3/websocket"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"

	"jambo-bingo/backend/internal/config"
	"jambo-bingo/backend/internal/database"
	"jambo-bingo/backend/internal/handlers"
	"jambo-bingo/backend/internal/middleware"
	"jambo-bingo/backend/internal/services"
	wshub "jambo-bingo/backend/internal/websocket"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize database
	db, err := database.New(&cfg.Database)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Initialize Redis
	redis, err := database.NewRedis(&cfg.Redis)
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	defer redis.Close()

	// Initialize services
	userService := services.NewUserService(db)
	walletService := services.NewWalletService(db)
	gameService := services.NewGameService(db, redis)

	// Initialize WebSocket hub
	hub := wshub.NewHub(db, redis, gameService, walletService)
	go hub.Run()

	// Initialize handlers
	userHandler := handlers.NewUserHandler(userService)
	walletHandler := handlers.NewWalletHandler(walletService)
	gameHandler := handlers.NewGameHandler(gameService)

	// Create Fiber app
	app := fiber.New(fiber.Config{
		AppName:      "Beteseb Bingo API",
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	})

	// Global middleware
	app.Use(recover.New())
	app.Use(logger.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins: []string{"http://localhost:5173","http://localhost:3001",},
AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
AllowHeaders: []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Telegram-Init-Data"},
		AllowCredentials: true,
	}))

	// Health check
	app.Get("/health", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status":    "healthy",
			"service":   "jambo-bingo",
			"timestamp": time.Now().Unix(),
		})
	})

		// API Routes
	api := app.Group("/api/v1")

	// Public routes (NO auth)
	api.Post("/auth/register", userHandler.Register)
	api.Post("/auth/verify", middleware.ValidateInitData(cfg.Telegram.BotToken), userHandler.VerifyInitData)

	// Bot routes (X-Bot-Service-Token only)
	botRoutes := api.Group("/bot")
	botRoutes.Use(middleware.BotAuthMiddleware(cfg.Security.BOTServiceToken))
	botRoutes.Get("/wallets/:user_id/balance", walletHandler.GetBalance)
	botRoutes.Get("/users/telegram/:telegram_id", userHandler.GetProfile)

	// Protected routes (JWT only) - use "/p" prefix, NOT "/"
	protected := api.Group("/p")
	protected.Use(middleware.JWTMiddleware(cfg.Security.JWTSecret))
	protected.Get("/users/profile", userHandler.GetProfile)
	protected.Get("/wallets/:user_id/balance", walletHandler.GetBalance)
	protected.Get("/wallets/:user_id/history", walletHandler.GetHistory)
	protected.Get("/games/active", gameHandler.GetActiveGame)
	protected.Get("/games/:user_id/history", gameHandler.GetHistory)
	protected.Get("/games/:user_id/stats", gameHandler.GetStats)
	// WebSocket route
	app.Use("/ws", func(c fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})
	app.Get("/ws", websocket.New(hub.HandleWebSocket))

	// Graceful shutdown
	go func() {
		port := cfg.Server.Port
		if port == "" {
			port = "8080"
		}
		log.Printf("🚀 Beteseb Bingo server starting on port %s", port)
		if err := app.Listen(":" + port); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	if err := app.Shutdown(); err != nil {
		log.Printf("Shutdown error: %v", err)
	}
	log.Println("Server exited")
}
