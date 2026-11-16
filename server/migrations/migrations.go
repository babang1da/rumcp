// Package migrations applies the SQL schema of the service.
//
// Files are named NNNNN_name.up.sql / NNNNN_name.down.sql and are embedded into
// the binary, so a deploy needs no extra tool. Applied versions are tracked in
// the schema_migrations table.
package migrations

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.sql
var files embed.FS

// migrationLock keeps two instances from applying the schema at the same time.
const migrationLock = 7_747_001

// Up applies every migration that is not applied yet.
func Up(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("migrations: acquire: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "select pg_advisory_lock($1)", migrationLock); err != nil {
		return fmt.Errorf("migrations: lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), "select pg_advisory_unlock($1)", migrationLock)
	}()

	if _, err := conn.Exec(ctx, `create table if not exists schema_migrations (
		version bigint primary key,
		name text not null,
		applied_at timestamptz not null default now()
	)`); err != nil {
		return fmt.Errorf("migrations: schema_migrations: %w", err)
	}

	applied := map[int64]bool{}
	rows, err := conn.Query(ctx, "select version from schema_migrations")
	if err != nil {
		return fmt.Errorf("migrations: applied versions: %w", err)
	}
	for rows.Next() {
		var version int64
		if err := rows.Scan(&version); err != nil {
			rows.Close()
			return fmt.Errorf("migrations: scan version: %w", err)
		}
		applied[version] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("migrations: applied versions: %w", err)
	}

	for _, m := range available() {
		if applied[m.version] {
			continue
		}
		body, err := files.ReadFile(m.up)
		if err != nil {
			return fmt.Errorf("migrations: read %s: %w", m.up, err)
		}
		if err := applyOne(ctx, conn, m, body); err != nil {
			return err
		}
	}
	return nil
}

// Down rolls back the last applied migration.
func Down(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("migrations: acquire: %w", err)
	}
	defer conn.Release()

	var version int64
	if err := conn.QueryRow(ctx, "select coalesce(max(version), 0) from schema_migrations").Scan(&version); err != nil {
		return fmt.Errorf("migrations: last version: %w", err)
	}
	if version == 0 {
		return nil
	}

	var m migration
	for _, candidate := range available() {
		if candidate.version == version {
			m = candidate
			break
		}
	}
	if m.down == "" {
		return fmt.Errorf("migrations: no down file for version %d", version)
	}
	body, err := files.ReadFile(m.down)
	if err != nil {
		return fmt.Errorf("migrations: read %s: %w", m.down, err)
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("migrations: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, string(body)); err != nil {
		return fmt.Errorf("migrations: apply %s: %w", m.down, err)
	}
	if _, err := tx.Exec(ctx, "delete from schema_migrations where version = $1", version); err != nil {
		return fmt.Errorf("migrations: drop version %d: %w", version, err)
	}
	return tx.Commit(ctx)
}

func applyOne(ctx context.Context, conn *pgxpool.Conn, m migration, body []byte) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("migrations: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if _, err := tx.Exec(ctx, string(body)); err != nil {
		return fmt.Errorf("migrations: apply %s: %w", m.up, err)
	}
	if _, err := tx.Exec(ctx,
		"insert into schema_migrations (version, name) values ($1, $2)", m.version, m.name); err != nil {
		return fmt.Errorf("migrations: record %s: %w", m.up, err)
	}
	return tx.Commit(ctx)
}

type migration struct {
	version int64
	name    string
	up      string
	down    string
}

// available lists the embedded migrations in version order.
func available() []migration {
	entries, err := files.ReadDir(".")
	if err != nil {
		return nil
	}
	byVersion := map[int64]*migration{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		parts := strings.SplitN(name, "_", 2)
		version, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			continue
		}
		base := strings.TrimSuffix(name, ".up.sql")
		base = strings.TrimSuffix(base, ".down.sql")
		rest := strings.TrimPrefix(base, parts[0]+"_")

		m, ok := byVersion[version]
		if !ok {
			m = &migration{version: version, name: rest}
			byVersion[version] = m
		}
		switch {
		case strings.HasSuffix(name, ".up.sql"):
			m.up = name
		case strings.HasSuffix(name, ".down.sql"):
			m.down = name
		}
	}

	result := make([]migration, 0, len(byVersion))
	for _, m := range byVersion {
		if m.up != "" {
			result = append(result, *m)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].version < result[j].version })
	return result
}
