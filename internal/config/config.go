package config

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
)

// Default constants
const (
	DefaultPort           = "8080"
	DefaultHost           = "0.0.0.0"
	DefaultLogLevel       = "info"
	DefaultDBURL          = "postgres://postgres:postgres@localhost:5433/agentdb"
	DefaultScanInterval   = 3600
	DefaultWorkerPoolSize = 4
	DefaultEmbeddingModel = "text-embedding-v3"
	DefaultChatModel      = "qwen-max"
)

// Errors
var (
	ErrMissingDBURL      = errors.New("DATABASE_URL is required")
	ErrMissingAlibabaKey = errors.New("ALIBABA_API_KEY is required")
)

// Config holds all configuration
type Config struct {
	Port           string
	Host           string
	LogLevel       string
	DatabaseURL    string
	AlibabaAPIKey  string
	EmbeddingModel string
	ChatModel      string
	ScanInterval   int
	WorkerPoolSize int
	WatchDirs      []string
}

// Load loads configuration from environment with const defaults
func Load() (*Config, error) {
	_ = godotenv.Load()

	scanInterval, _ := strconv.Atoi(getEnv("SCAN_INTERVAL_SECONDS", strconv.Itoa(DefaultScanInterval)))
	if scanInterval <= 0 {
		scanInterval = DefaultScanInterval
	}

	workerPoolSize, _ := strconv.Atoi(getEnv("WORKER_POOL_SIZE", strconv.Itoa(DefaultWorkerPoolSize)))
	if workerPoolSize <= 0 {
		workerPoolSize = DefaultWorkerPoolSize
	}

	cfg := &Config{
		Port:           getEnv("API_PORT", DefaultPort),
		Host:           getEnv("API_HOST", DefaultHost),
		LogLevel:       getEnv("LOG_LEVEL", DefaultLogLevel),
		DatabaseURL:    getEnv("DATABASE_URL", DefaultDBURL),
		AlibabaAPIKey:  os.Getenv("ALIBABA_API_KEY"),
		EmbeddingModel: getEnv("ALIBABA_EMBEDDING_MODEL", DefaultEmbeddingModel),
		ChatModel:      getEnv("ALIBABA_CHAT_MODEL", DefaultChatModel),
		ScanInterval:   scanInterval,
		WorkerPoolSize: workerPoolSize,
		WatchDirs:      splitDirs(os.Getenv("WATCH_DIRS")),
	}

	return cfg, cfg.Validate()
}

// Validate checks required configuration
func (c *Config) Validate() error {
	if c.DatabaseURL == "" {
		return ErrMissingDBURL
	}
	if c.AlibabaAPIKey == "" {
		return ErrMissingAlibabaKey
	}
	return nil
}

// SetupLogging configures logrus based on config
func SetupLogging(level string) {
	lvl, err := logrus.ParseLevel(level)
	if err != nil {
		lvl = logrus.InfoLevel
	}
	logrus.SetLevel(lvl)
	logrus.SetFormatter(&logrus.JSONFormatter{})
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func splitDirs(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
