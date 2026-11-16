// Package config читает настройки сервиса из переменных окружения.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
)

// Config — настройки сервиса. Значения приходят из env, в коде только умолчания.
type Config struct {
	// HTTPAddr — адрес прослушивания, собирается из PORT.
	HTTPAddr string
	// DatabaseURL — строка подключения к Postgres, владельцем схемы является этот сервис.
	DatabaseURL string
	// LogLevel — уровень логирования slog.
	LogLevel slog.Level
}

// FromEnv читает конфигурацию из окружения. PORT и LOG_LEVEL необязательны,
// DATABASE_URL обязателен.
func FromEnv() (Config, error) {
	cfg := Config{
		HTTPAddr:    ":8080",
		DatabaseURL: os.Getenv("DATABASE_URL"),
		LogLevel:    slog.LevelInfo,
	}

	if port := os.Getenv("PORT"); port != "" {
		if err := validatePort(port); err != nil {
			return Config{}, err
		}
		cfg.HTTPAddr = ":" + port
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("config: DATABASE_URL is required")
	}

	if level := os.Getenv("LOG_LEVEL"); level != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(level)); err != nil {
			return Config{}, fmt.Errorf("config: LOG_LEVEL: %w", err)
		}
	}

	return cfg, nil
}

func validatePort(port string) error {
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("config: PORT must be a number between 1 and 65535, got %q", port)
	}
	return nil
}
