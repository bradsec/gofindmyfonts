package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	DefaultPort             = "8080"
	DefaultMaxConcurrent    = 4
	DefaultPreviewCacheTime = 24 * time.Hour
	DefaultFontSize         = 48.0
	DefaultMaxFileSize      = 50 * 1024 * 1024 // 50MB
)

type Config struct {
	Port             string
	StaticDir        string
	LogDir           string
	MaxConcurrent    int
	PreviewCacheTime time.Duration
	FontSize         float64
	MaxFileSize      int64
}

func LoadConfig() *Config {
	config := &Config{
		Port:             getEnvOrDefault("PORT", DefaultPort),
		StaticDir:        filepath.Join(".", "static"),
		LogDir:           filepath.Join(".", "logs"),
		MaxConcurrent:    getEnvIntOrDefault("MAX_CONCURRENT", DefaultMaxConcurrent),
		PreviewCacheTime: DefaultPreviewCacheTime,
		FontSize:         DefaultFontSize,
		MaxFileSize:      DefaultMaxFileSize,
	}

	if maxSize := os.Getenv("MAX_FILE_SIZE"); maxSize != "" {
		if size, err := strconv.ParseInt(maxSize, 10, 64); err == nil {
			config.MaxFileSize = size
		}
	}

	return config
}

func (c *Config) Validate() error {
	if c.Port == "" {
		return fmt.Errorf("port cannot be empty")
	}

	if c.MaxConcurrent < 1 {
		return fmt.Errorf("maxConcurrent must be at least 1")
	}

	if c.FontSize <= 0 {
		return fmt.Errorf("fontSize must be positive")
	}

	// Ensure directories exist
	dirs := []string{c.StaticDir, c.LogDir}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %v", dir, err)
		}
	}

	return nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvIntOrDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}
