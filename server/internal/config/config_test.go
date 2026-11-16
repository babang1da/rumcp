package config

import (
	"log/slog"
	"testing"
)

func TestFromEnvDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://rumcp:rumcp@localhost:5432/rumcp?sslmode=disable")
	t.Setenv("PORT", "")
	t.Setenv("LOG_LEVEL", "")

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
}

func TestFromEnvOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://rumcp:rumcp@postgres:5432/rumcp?sslmode=disable")
	t.Setenv("PORT", "9000")
	t.Setenv("LOG_LEVEL", "debug")

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
}

func TestFromEnvRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	if _, err := FromEnv(); err == nil {
		t.Fatal("FromEnv() error = nil, want error for missing DATABASE_URL")
	}
}

func TestFromEnvRejectsBadPort(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://rumcp:rumcp@localhost:5432/rumcp?sslmode=disable")

	for _, port := range []string{"abc", "0", "70000"} {
		t.Setenv("PORT", port)
		if _, err := FromEnv(); err == nil {
			t.Errorf("FromEnv() with PORT=%q error = nil, want error", port)
		}
	}
}
