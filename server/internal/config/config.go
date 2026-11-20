// Package config читает настройки сервиса из переменных окружения.
package config

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// Config — настройки сервиса. Значения приходят из env, в коде только умолчания.
type Config struct {
	// HTTPAddr — адрес прослушивания, собирается из PORT.
	HTTPAddr string
	// DatabaseURL — строка подключения к Postgres, владельцем схемы является этот сервис.
	DatabaseURL string
	// LogLevel — уровень логирования slog.
	LogLevel slog.Level

	// BaseURL — публичный адрес сервиса: из него собираются OAuth-метаданные и redirect_uri.
	BaseURL string
	// TokenKey — ключ AES-256 для токенов Яндекса (TOKEN_ENCRYPTION_KEY).
	TokenKey []byte
	// SessionSecret — ключ подписи cookie кабинета (SESSION_SECRET).
	SessionSecret []byte
	// YandexClientID и YandexClientSecret — приложение Яндекса; пусто, пока его нет (ZHC-877).
	YandexClientID     string
	YandexClientSecret string
	// YandexTestToken — токен Яндекса для dev и тестов, работает только без приложения.
	YandexTestToken string
	// DailyLimit — вызовов MCP на пользователя в сутки (DAILY_LIMIT).
	DailyLimit int
}

// FromEnv читает конфигурацию из окружения. PORT и LOG_LEVEL необязательны,
// DATABASE_URL обязателен, как и секреты: без них сервис не стартует.
func FromEnv() (Config, error) {
	cfg := Config{
		HTTPAddr:           ":8080",
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		LogLevel:           slog.LevelInfo,
		BaseURL:            strings.TrimSuffix(envOr("BASE_URL", "http://localhost:8080"), "/"),
		YandexClientID:     os.Getenv("YANDEX_CLIENT_ID"),
		YandexClientSecret: os.Getenv("YANDEX_CLIENT_SECRET"),
		YandexTestToken:    os.Getenv("YANDEX_TEST_TOKEN"),
		DailyLimit:         100,
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

	if raw := os.Getenv("DAILY_LIMIT"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			return Config{}, fmt.Errorf("config: DAILY_LIMIT must be a positive integer, got %q", raw)
		}
		cfg.DailyLimit = limit
	}

	key, err := parseKey(os.Getenv("TOKEN_ENCRYPTION_KEY"))
	if err != nil {
		return Config{}, fmt.Errorf("config: TOKEN_ENCRYPTION_KEY: %w", err)
	}
	cfg.TokenKey = key

	secret := os.Getenv("SESSION_SECRET")
	if len(secret) < 16 {
		return Config{}, errors.New("config: SESSION_SECRET is required and must be at least 16 characters")
	}
	cfg.SessionSecret = []byte(secret)

	return cfg, nil
}

// Secure сообщает, что cookie можно отправлять только по HTTPS.
func (c Config) Secure() bool {
	return strings.HasPrefix(c.BaseURL, "https://")
}

// DevLoginEnabled сообщает, можно ли входить по YANDEX_TEST_TOKEN вместо
// приложения Яндекса. Включается только пока приложение не настроено, поэтому
// развёрнутый сервис на тестовый токен не откатится.
func (c Config) DevLoginEnabled() bool {
	return c.YandexTestToken != "" && c.YandexClientID == ""
}

func validatePort(port string) error {
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("config: PORT must be a number between 1 and 65535, got %q", port)
	}
	return nil
}

// parseKey принимает ключ AES-256 в base64, hex или 32 сырыми байтами.
func parseKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("is required")
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.RawURLEncoding.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := hex.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	if len(raw) == 32 {
		return []byte(raw), nil
	}
	return nil, errors.New("must be 32 bytes encoded as base64, hex or raw")
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
