package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres implements Repository on top of a pgx pool.
type Postgres struct {
	pool *pgxpool.Pool
}

// Open connects to Postgres and verifies the connection.
func Open(ctx context.Context, dsn string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	return &Postgres{pool: pool}, nil
}

// Close releases the connection pool.
func (p *Postgres) Close() { p.pool.Close() }

// Pool exposes the underlying pool for migrations.
func (p *Postgres) Pool() *pgxpool.Pool { return p.pool }

// Ping checks that the database is reachable.
func (p *Postgres) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }

// UpsertUser creates or refreshes the user record of a Yandex account.
func (p *Postgres) UpsertUser(ctx context.Context, u User) (User, error) {
	const query = `
		insert into users (yandex_id, login, email, name)
		values ($1, $2, $3, $4)
		on conflict (yandex_id) do update
		set login = excluded.login,
		    email = excluded.email,
		    name = excluded.name,
		    updated_at = now()
		returning id::text, yandex_id, login, email, name, created_at`
	return scanUser(p.pool.QueryRow(ctx, query, u.YandexID, u.Login, u.Email, u.Name))
}

// UserByID loads a user by the internal id.
func (p *Postgres) UserByID(ctx context.Context, id string) (User, error) {
	const query = `select id::text, yandex_id, login, email, name, created_at from users where id = $1::uuid`
	return scanUser(p.pool.QueryRow(ctx, query, id))
}

// SaveAccount stores the sealed Yandex tokens of a user.
func (p *Postgres) SaveAccount(ctx context.Context, a Account) error {
	const query = `
		insert into accounts (user_id, provider, access_token_enc, refresh_token_enc, scope, expires_at)
		values ($1::uuid, $2, $3, $4, $5, $6)
		on conflict (user_id, provider) do update
		set access_token_enc = excluded.access_token_enc,
		    refresh_token_enc = excluded.refresh_token_enc,
		    scope = excluded.scope,
		    expires_at = excluded.expires_at,
		    updated_at = now()`
	_, err := p.pool.Exec(ctx, query, a.UserID, a.Provider, a.AccessTokenEnc, a.RefreshTokenEnc, a.Scope, a.ExpiresAt)
	if err != nil {
		return fmt.Errorf("store: save account: %w", err)
	}
	return nil
}

// AccountByUser loads the connected account of a user.
func (p *Postgres) AccountByUser(ctx context.Context, userID, provider string) (Account, error) {
	const query = `
		select user_id::text, provider, access_token_enc, coalesce(refresh_token_enc, ''), scope, expires_at, created_at
		from accounts where user_id = $1::uuid and provider = $2`
	var a Account
	err := p.pool.QueryRow(ctx, query, userID, provider).Scan(
		&a.UserID, &a.Provider, &a.AccessTokenEnc, &a.RefreshTokenEnc, &a.Scope, &a.ExpiresAt, &a.ConnectedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("store: account by user: %w", err)
	}
	return a, nil
}

// DeleteAccount disconnects the account of a user.
func (p *Postgres) DeleteAccount(ctx context.Context, userID, provider string) error {
	_, err := p.pool.Exec(ctx, `delete from accounts where user_id = $1::uuid and provider = $2`, userID, provider)
	if err != nil {
		return fmt.Errorf("store: delete account: %w", err)
	}
	return nil
}

// CreateClient registers an OAuth client.
func (p *Postgres) CreateClient(ctx context.Context, c OAuthClient) error {
	const query = `
		insert into oauth_clients (client_id, client_secret_hash, client_name, redirect_uris, grant_types, response_types, token_endpoint_auth_method)
		values ($1, nullif($2, ''), $3, $4, $5, $6, $7)`
	_, err := p.pool.Exec(ctx, query, c.ClientID, c.ClientSecretHash, c.ClientName,
		c.RedirectURIs, c.GrantTypes, c.ResponseTypes, c.TokenEndpointAuthMethod)
	if err != nil {
		return fmt.Errorf("store: create client: %w", err)
	}
	return nil
}

// ClientByID loads a registered OAuth client.
func (p *Postgres) ClientByID(ctx context.Context, clientID string) (OAuthClient, error) {
	const query = `
		select client_id, coalesce(client_secret_hash, ''), client_name, redirect_uris,
		       grant_types, response_types, token_endpoint_auth_method, created_at
		from oauth_clients where client_id = $1`
	var c OAuthClient
	err := p.pool.QueryRow(ctx, query, clientID).Scan(&c.ClientID, &c.ClientSecretHash, &c.ClientName,
		&c.RedirectURIs, &c.GrantTypes, &c.ResponseTypes, &c.TokenEndpointAuthMethod, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return OAuthClient{}, ErrNotFound
	}
	if err != nil {
		return OAuthClient{}, fmt.Errorf("store: client by id: %w", err)
	}
	return c, nil
}

// CreateToken stores an authorization code or token hash.
func (p *Postgres) CreateToken(ctx context.Context, t OAuthToken) error {
	const query = `
		insert into oauth_tokens (user_id, client_id, kind, token_hash, code_challenge, code_challenge_method, redirect_uri, scope, expires_at)
		values ($1::uuid, $2, $3, $4, nullif($5, ''), nullif($6, ''), nullif($7, ''), $8, $9)`
	_, err := p.pool.Exec(ctx, query, t.UserID, t.ClientID, t.Kind, t.TokenHash,
		t.CodeChallenge, t.CodeChallengeMethod, t.RedirectURI, t.Scope, t.ExpiresAt)
	if err != nil {
		return fmt.Errorf("store: create token: %w", err)
	}
	return nil
}

// TokenByHash loads a token by its SHA-256 hash and kind.
func (p *Postgres) TokenByHash(ctx context.Context, hash, kind string) (OAuthToken, error) {
	const query = `
		select id::text, user_id::text, client_id, kind, token_hash, coalesce(code_challenge, ''),
		       coalesce(code_challenge_method, ''), coalesce(redirect_uri, ''), scope, expires_at, consumed_at, created_at
		from oauth_tokens where token_hash = $1 and kind = $2`
	var t OAuthToken
	err := p.pool.QueryRow(ctx, query, hash, kind).Scan(&t.ID, &t.UserID, &t.ClientID, &t.Kind, &t.TokenHash,
		&t.CodeChallenge, &t.CodeChallengeMethod, &t.RedirectURI, &t.Scope, &t.ExpiresAt, &t.ConsumedAt, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return OAuthToken{}, ErrNotFound
	}
	if err != nil {
		return OAuthToken{}, fmt.Errorf("store: token by hash: %w", err)
	}
	return t, nil
}

// ConsumeToken marks a token or code as used. It fails when the row was
// already consumed, so two concurrent exchanges cannot both win.
func (p *Postgres) ConsumeToken(ctx context.Context, id string) error {
	tag, err := p.pool.Exec(ctx, `update oauth_tokens set consumed_at = now() where id = $1::uuid and consumed_at is null`, id)
	if err != nil {
		return fmt.Errorf("store: consume token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTokenConsumed
	}
	return nil
}

// RecordCall writes one MCP tool call to the call journal.
func (p *Postgres) RecordCall(ctx context.Context, userID, connector, tool, status string) error {
	_, err := p.pool.Exec(ctx,
		`insert into call_log (user_id, connector, tool, status) values ($1::uuid, $2, $3, $4)`,
		userID, connector, tool, status)
	if err != nil {
		return fmt.Errorf("store: record call: %w", err)
	}
	return nil
}

// StartCall opens a call journal row and returns its id.
func (p *Postgres) StartCall(ctx context.Context, userID, connector, tool, status string) (int64, error) {
	var id int64
	err := p.pool.QueryRow(ctx,
		`insert into call_log (user_id, connector, tool, status) values ($1::uuid, $2, $3, $4) returning id`,
		userID, connector, tool, status).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("store: start call: %w", err)
	}
	return id, nil
}

// FinishCall sets the status of a call journal row.
func (p *Postgres) FinishCall(ctx context.Context, id int64, status string) error {
	_, err := p.pool.Exec(ctx, `update call_log set status = $2 where id = $1`, id, status)
	if err != nil {
		return fmt.Errorf("store: finish call: %w", err)
	}
	return nil
}

// Usage sums the calls of a user since the given moment.
func (p *Postgres) Usage(ctx context.Context, userID string, since time.Time) (Usage, error) {
	rows, err := p.pool.Query(ctx,
		`select connector, count(*) from call_log where user_id = $1::uuid and ts >= $2 group by connector`,
		userID, since)
	if err != nil {
		return Usage{}, fmt.Errorf("store: usage: %w", err)
	}
	defer rows.Close()

	usage := Usage{ByConnector: map[string]int{}}
	for rows.Next() {
		var connector string
		var count int
		if err := rows.Scan(&connector, &count); err != nil {
			return Usage{}, fmt.Errorf("store: usage scan: %w", err)
		}
		usage.ByConnector[connector] = count
		usage.CallsToday += count
	}
	if err := rows.Err(); err != nil {
		return Usage{}, fmt.Errorf("store: usage rows: %w", err)
	}
	return usage, nil
}

// CreateLead stores a request for a connector.
func (p *Postgres) CreateLead(ctx context.Context, l Lead) error {
	const query = `insert into leads (email, service, user_id) values ($1, $2, nullif($3, '')::uuid)`
	if _, err := p.pool.Exec(ctx, query, l.Email, l.Service, l.UserID); err != nil {
		return fmt.Errorf("store: create lead: %w", err)
	}
	return nil
}

// Stats returns overall call counts and per-user call aggregates.
func (p *Postgres) Stats(ctx context.Context) (Stats, error) {
	var totalCalls int64
	if err := p.pool.QueryRow(ctx, `select count(*) from call_log`).Scan(&totalCalls); err != nil {
		return Stats{}, fmt.Errorf("store: stats total calls: %w", err)
	}

	const query = `
		select u.id::text, u.login, u.email, count(c.id), min(c.ts), max(c.ts)
		from call_log c
		join users u on u.id = c.user_id
		group by u.id, u.login, u.email
		order by min(c.ts) asc, u.id asc`

	rows, err := p.pool.Query(ctx, query)
	if err != nil {
		return Stats{}, fmt.Errorf("store: stats users: %w", err)
	}
	defer rows.Close()

	users := make([]UserStats, 0)
	for rows.Next() {
		var u UserStats
		if err := rows.Scan(&u.UserID, &u.Login, &u.Email, &u.Calls, &u.FirstCallAt, &u.LastCallAt); err != nil {
			return Stats{}, fmt.Errorf("store: stats scan: %w", err)
		}
		u.FirstCallAt = u.FirstCallAt.UTC()
		u.LastCallAt = u.LastCallAt.UTC()
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return Stats{}, fmt.Errorf("store: stats rows: %w", err)
	}

	return Stats{
		TotalCalls: totalCalls,
		Users:      users,
	}, nil
}

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.YandexID, &u.Login, &u.Email, &u.Name, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("store: scan user: %w", err)
	}
	return u, nil
}
