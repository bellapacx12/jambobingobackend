package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9" // Add this import
)

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Redis    RedisConfig
	Telegram TelegramConfig
	Security SecurityConfig
	Game     GameConfig
}

type ServerConfig struct {
	Port string
	Env  string
}

type DatabaseConfig struct {
	URL        string
	MaxConns   int32
	MinConns   int32
	MaxConnTTL time.Duration
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type TelegramConfig struct {
	BotToken string
	BotName  string
}

type SecurityConfig struct {
	JWTSecret   string
	HMACSecret  string
}

type GameConfig struct {
	LobbyDuration      time.Duration
	BallDropMinMs      time.Duration
	BallDropMaxMs      time.Duration
	MaxPlayersPerRoom  int
	PrizePoolMultiplier float64
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{}

	// Server
	cfg.Server.Port = getEnv("SERVER_PORT", "8080")
	cfg.Server.Env = getEnv("SERVER_ENV", "development")

	// Database
	cfg.Database.URL = getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/beteseb_bingo?sslmode=disable")
	cfg.Database.MaxConns = int32(getEnvInt("DB_MAX_CONNS", 25))
	cfg.Database.MinConns = int32(getEnvInt("DB_MIN_CONNS", 5))
	cfg.Database.MaxConnTTL = time.Hour

	// Redis - Parse URL properly
	redisURL := os.Getenv("REDIS_URL")
	if redisURL != "" {
		// Parse the Redis URL to extract components
		opts, err := redis.ParseURL(redisURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse REDIS_URL: %w", err)
		}
		cfg.Redis.Addr = opts.Addr
		cfg.Redis.Password = opts.Password
		cfg.Redis.DB = opts.DB
	} else {
		// Fallback to individual environment variables or defaults
		cfg.Redis.Addr = getEnv("REDIS_ADDR", "localhost:6379")
		cfg.Redis.Password = getEnv("REDIS_PASSWORD", "")
		cfg.Redis.DB = getEnvInt("REDIS_DB", 0)
	}

	// Telegram
	cfg.Telegram.BotToken = getEnv("TELEGRAM_BOT_TOKEN", "")
	cfg.Telegram.BotName = getEnv("TELEGRAM_BOT_USERNAME", "")

	// Security
	cfg.Security.JWTSecret = getEnv("JWT_SECRET", "change-me-in-production")
	cfg.Security.HMACSecret = getEnv("HMAC_SECRET", "change-me-in-production")

	// Game
	cfg.Game.LobbyDuration = time.Duration(getEnvInt("GAME_LOBBY_DURATION_SECONDS", 30)) * time.Second
	cfg.Game.BallDropMinMs = time.Duration(getEnvInt("GAME_BALL_DROP_INTERVAL_MIN_MS", 3000)) * time.Millisecond
	cfg.Game.BallDropMaxMs = time.Duration(getEnvInt("GAME_BALL_DROP_INTERVAL_MAX_MS", 5000)) * time.Millisecond
	cfg.Game.MaxPlayersPerRoom = getEnvInt("GAME_MAX_PLAYERS_PER_ROOM", 500)
	cfg.Game.PrizePoolMultiplier = getEnvFloat("GAME_PRIZE_POOL_MULTIPLIER", 0.85)

	if cfg.Telegram.BotToken == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if v, err := strconv.Atoi(value); err == nil {
			return v
		}
	}
	return defaultValue
}

func getEnvFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if v, err := strconv.ParseFloat(value, 64); err == nil {
			return v
		}
	}
	return defaultValue
}