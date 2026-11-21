package config

import (
	"encoding/base64"
	"log/slog"
	"testing"
)

// setCleanEnv выставляет переменные сервиса и обнуляет всё, что читает FromEnv,
// чтобы тест не зависел от окружения машины.
func setCleanEnv(t *testing.T, values map[string]string) {
	t.Helper()
	for _, name := range []string{"DATABASE_URL", "PORT", "LOG_LEVEL", "BASE_URL", "TOKEN_ENCRYPTION_KEY",
		"SESSION_SECRET", "YANDEX_CLIENT_ID", "YANDEX_CLIENT_SECRET", "YANDEX_TEST_TOKEN", "DAILY_LIMIT", "STATS_TOKEN"} {
		t.Setenv(name, "")
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
}

// minimalEnv — окружение, которого достаточно для успешного FromEnv.
func minimalEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL":         "postgres://rumcp:rumcp@localhost:5432/rumcp?sslmode=disable",
		"TOKEN_ENCRYPTION_KEY": base64.StdEncoding.EncodeToString(make([]byte, 32)),
		"SESSION_SECRET":       "0123456789abcdef",
	}
}

func TestFromEnvDefaults(t *testing.T) {
	setCleanEnv(t, minimalEnv())

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}

	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, ":8080")
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, slog.LevelInfo)
	}
	if cfg.BaseURL != "http://localhost:8080" {
		t.Errorf("BaseURL = %q, want %q", cfg.BaseURL, "http://localhost:8080")
	}
	if cfg.DailyLimit != 100 {
		t.Errorf("DailyLimit = %d, want 100", cfg.DailyLimit)
	}
	if cfg.Secure() {
		t.Error("Secure() = true for an http BaseURL")
	}
	if cfg.DevLoginEnabled() {
		t.Error("DevLoginEnabled() = true without YANDEX_TEST_TOKEN")
	}
}

func TestFromEnvOverrides(t *testing.T) {
	env := minimalEnv()
	env["DATABASE_URL"] = "postgres://rumcp:rumcp@postgres:5432/rumcp?sslmode=disable"
	env["PORT"] = "9000"
	env["LOG_LEVEL"] = "debug"
	env["BASE_URL"] = "https://rumcp.ru/"
	env["DAILY_LIMIT"] = "42"
	setCleanEnv(t, env)

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}

	if cfg.HTTPAddr != ":9000" {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, ":9000")
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, slog.LevelDebug)
	}
	if cfg.BaseURL != "https://rumcp.ru" {
		t.Errorf("BaseURL = %q, want no trailing slash", cfg.BaseURL)
	}
	if cfg.DailyLimit != 42 {
		t.Errorf("DailyLimit = %d, want 42", cfg.DailyLimit)
	}
	if !cfg.Secure() {
		t.Error("Secure() = false for an https BaseURL")
	}
}

func TestFromEnvRequiresDatabaseURL(t *testing.T) {
	env := minimalEnv()
	delete(env, "DATABASE_URL")
	setCleanEnv(t, env)

	if _, err := FromEnv(); err == nil {
		t.Fatal("FromEnv() error = nil, want error for missing DATABASE_URL")
	}
}

func TestFromEnvRequiresSecrets(t *testing.T) {
	for _, name := range []string{"TOKEN_ENCRYPTION_KEY", "SESSION_SECRET"} {
		env := minimalEnv()
		delete(env, name)
		setCleanEnv(t, env)
		if _, err := FromEnv(); err == nil {
			t.Errorf("FromEnv() error = nil, want error for missing %s", name)
		}
	}
}

func TestFromEnvRejectsBadPort(t *testing.T) {
	setCleanEnv(t, minimalEnv())

	for _, port := range []string{"abc", "0", "70000"} {
		t.Setenv("PORT", port)
		if _, err := FromEnv(); err == nil {
			t.Errorf("FromEnv() with PORT=%q error = nil, want error", port)
		}
	}
}

func TestFromEnvTokenKeyFormats(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i)
	}
	formats := map[string]string{
		"base64":  base64.StdEncoding.EncodeToString(raw),
		"raw-url": base64.RawURLEncoding.EncodeToString(raw),
		"hex":     "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f",
		"plain":   "0123456789abcdef0123456789abcdef",
	}
	for name, value := range formats {
		env := minimalEnv()
		env["TOKEN_ENCRYPTION_KEY"] = value
		setCleanEnv(t, env)
		cfg, err := FromEnv()
		if err != nil {
			t.Errorf("%s: FromEnv() error = %v", name, err)
			continue
		}
		if len(cfg.TokenKey) != 32 {
			t.Errorf("%s: key length %d, want 32", name, len(cfg.TokenKey))
		}
	}
}

func TestDevLoginNeedsTestTokenAndNoApplication(t *testing.T) {
	env := minimalEnv()
	env["YANDEX_TEST_TOKEN"] = "test-token"
	setCleanEnv(t, env)

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if !cfg.DevLoginEnabled() {
		t.Fatal("DevLoginEnabled() = false with a test token and no application")
	}

	env["YANDEX_CLIENT_ID"] = "app-id"
	env["YANDEX_CLIENT_SECRET"] = "app-secret"
	setCleanEnv(t, env)

	cfg, err = FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if cfg.DevLoginEnabled() {
		t.Fatal("DevLoginEnabled() = true while a Yandex application is configured")
	}
}

func TestFromEnvStatsToken(t *testing.T) {
	env := minimalEnv()
	setCleanEnv(t, env)

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() without STATS_TOKEN error = %v", err)
	}
	if cfg.StatsToken != "" {
		t.Errorf("StatsToken = %q, want empty string", cfg.StatsToken)
	}

	env["STATS_TOKEN"] = "secret-token-123"
	setCleanEnv(t, env)

	cfg, err = FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() with STATS_TOKEN error = %v", err)
	}
	if cfg.StatsToken != "secret-token-123" {
		t.Errorf("StatsToken = %q, want %q", cfg.StatsToken, "secret-token-123")
	}
}
