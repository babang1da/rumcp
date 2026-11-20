// Command migrate applies the SQL schema of the service.
//
//	go run ./cmd/migrate up      apply all pending migrations
//	go run ./cmd/migrate down    roll back the last migration
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/babang1da/rumcp/server/internal/store"
	"github.com/babang1da/rumcp/server/migrations"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run() error {
	command := "up"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	repo, err := store.Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer repo.Close()

	switch command {
	case "up":
		return migrations.Up(ctx, repo.Pool())
	case "down":
		return migrations.Down(ctx, repo.Pool())
	default:
		return fmt.Errorf("unknown command %q, expected up or down", command)
	}
}
