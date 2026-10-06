package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config holds runtime configuration loaded from the environment.
type Config struct {
	BaseURL       string
	ListenAddr    string
	DatabasePath  string
	DataDir       string
	SessionSecret string
}

// Load reads configuration from environment variables.
func Load() (Config, error) {
	cfg := Config{
		BaseURL:       strings.TrimRight(envOr("BASE_URL", "http://localhost:8080"), "/"),
		ListenAddr:    envOr("LISTEN_ADDR", "0.0.0.0:8080"),
		DataDir:       envOr("DATA_DIR", "/data"),
		DatabasePath:  os.Getenv("DATABASE_PATH"),
		SessionSecret: os.Getenv("SESSION_SECRET"),
	}
	if cfg.DatabasePath == "" {
		cfg.DatabasePath = filepath.Join(cfg.DataDir, "paste.sqlite")
	}
	if cfg.BaseURL == "" {
		return Config{}, fmt.Errorf("BASE_URL must not be empty")
	}
	if cfg.ListenAddr == "" {
		return Config{}, fmt.Errorf("LISTEN_ADDR must not be empty")
	}
	if cfg.DatabasePath == "" {
		return Config{}, fmt.Errorf("DATABASE_PATH must not be empty")
	}
	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
