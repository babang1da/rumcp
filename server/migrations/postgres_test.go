package migrations_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/babang1da/rumcp/server/internal/auth"
	"github.com/babang1da/rumcp/server/internal/secret"
	"github.com/babang1da/rumcp/server/internal/store"
	"github.com/babang1da/rumcp/server/migrations"
)

// These tests need a disposable Postgres, e.g. the compose database:
//
//	TEST_DATABASE_URL=postgres://rumcp:rumcp@localhost:5432/rumcp?sslmode=disable go test ./...
func testStore(t *testing.T) *store.Postgres {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()

	repo, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(repo.Close)

	// Every test starts from a clean schema, as a fresh database would.
	if _, err := repo.Pool().Exec(ctx, "drop schema public cascade; create schema public"); err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	return repo
}

func TestMigrationsApplyOnCleanDatabase(t *testing.T) {
	repo := testStore(t)
	ctx := context.Background()

	if err := migrations.Up(ctx, repo.Pool()); err != nil {
		t.Fatalf("migrations.Up: %v", err)
	}
	// A second run changes nothing.
	if err := migrations.Up(ctx, repo.Pool()); err != nil {
		t.Fatalf("second migrations.Up: %v", err)
	}

	for _, table := range []string{"users", "accounts", "oauth_clients", "oauth_tokens", "call_log", "leads"} {
		var exists bool
		err := repo.Pool().QueryRow(ctx,
			"select exists (select 1 from information_schema.tables where table_schema = 'public' and table_name = $1)",
			table).Scan(&exists)
		if err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %s was not created", table)
		}
	}

	var version int64
	if err := repo.Pool().QueryRow(ctx, "select max(version) from schema_migrations").Scan(&version); err != nil {
		t.Fatalf("schema_migrations: %v", err)
	}
	if version != 2 {
		t.Errorf("schema version = %d, want 2 (0001_init + 0002_auth)", version)
	}

	// Down restores the empty schema.
	if err := migrations.Down(ctx, repo.Pool()); err != nil {
		t.Fatalf("migrations.Down: %v", err)
	}
	var exists bool
	if err := repo.Pool().QueryRow(ctx,
		"select exists (select 1 from information_schema.tables where table_schema = 'public' and table_name = 'users')").Scan(&exists); err != nil {
		t.Fatalf("check users after down: %v", err)
	}
	if exists {
		t.Error("table users survived migrations.Down")
	}
}

func TestYandexTokenIsSealedInDatabase(t *testing.T) {
	repo := testStore(t)
	ctx := context.Background()
	if err := migrations.Up(ctx, repo.Pool()); err != nil {
		t.Fatalf("migrations.Up: %v", err)
	}

	box, err := secret.NewBox([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("secret.NewBox: %v", err)
	}
	const plaintext = "yandex-access-token-1"
	handler := &auth.Handler{
		Repo:    repo,
		Yandex:  fakeYandex{token: plaintext},
		Session: auth.NewSession([]byte("session-secret-long-enough"), false),
		Box:     box,
		Cfg:     auth.Config{BaseURL: "http://localhost:8080", DevLogin: true, TestToken: plaintext},
	}
	recorder := httptest.NewRecorder()
	handler.Login(recorder, httptest.NewRequest(http.MethodGet, "/oauth/yandex/login", nil))
	if recorder.Code != http.StatusFound {
		t.Fatalf("login status %d, want 302", recorder.Code)
	}

	var stored string
	if err := repo.Pool().QueryRow(ctx, "select access_token_enc from accounts").Scan(&stored); err != nil {
		t.Fatalf("read stored token: %v", err)
	}
	if stored == plaintext || strings.Contains(stored, plaintext) {
		t.Fatalf("database stores the plaintext token: %q", stored)
	}
	opened, err := box.Open(stored)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if opened != plaintext {
		t.Fatalf("stored token opens to %q, want %q", opened, plaintext)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	repo := testStore(t)
	ctx := context.Background()
	if err := migrations.Up(ctx, repo.Pool()); err != nil {
		t.Fatalf("migrations.Up: %v", err)
	}

	user, err := repo.UpsertUser(ctx, store.User{YandexID: "100500", Login: "egor", Email: "egor@example.com", Name: "Egor"})
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	again, err := repo.UpsertUser(ctx, store.User{YandexID: "100500", Login: "egor2", Email: "egor@example.com"})
	if err != nil {
		t.Fatalf("second UpsertUser: %v", err)
	}
	if again.ID != user.ID {
		t.Fatalf("UpsertUser created a second user for one Yandex id")
	}
	if again.Login != "egor2" {
		t.Errorf("login = %q, want the refreshed value", again.Login)
	}

	if err := repo.SaveAccount(ctx, store.Account{UserID: user.ID, Provider: "yandex", AccessTokenEnc: "v1:sealed"}); err != nil {
		t.Fatalf("SaveAccount: %v", err)
	}
	account, err := repo.AccountByUser(ctx, user.ID, "yandex")
	if err != nil {
		t.Fatalf("AccountByUser: %v", err)
	}
	if account.AccessTokenEnc != "v1:sealed" {
		t.Errorf("access token = %q", account.AccessTokenEnc)
	}

	if err := repo.CreateClient(ctx, store.OAuthClient{
		ClientID: "rumcp_test", ClientName: "Test", RedirectURIs: []string{"http://localhost/cb"},
		GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"},
		TokenEndpointAuthMethod: "none",
	}); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	client, err := repo.ClientByID(ctx, "rumcp_test")
	if err != nil {
		t.Fatalf("ClientByID: %v", err)
	}
	if len(client.RedirectURIs) != 1 || client.RedirectURIs[0] != "http://localhost/cb" {
		t.Errorf("redirect uris = %v", client.RedirectURIs)
	}

	expires := user.CreatedAt.Add(3600 * 1e9)
	if err := repo.CreateToken(ctx, store.OAuthToken{
		UserID: user.ID, ClientID: client.ClientID, Kind: store.TokenKindAccess,
		TokenHash: "hash-1", Scope: "mcp", ExpiresAt: expires,
	}); err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	token, err := repo.TokenByHash(ctx, "hash-1", store.TokenKindAccess)
	if err != nil {
		t.Fatalf("TokenByHash: %v", err)
	}
	if token.ConsumedAt != nil {
		t.Error("new token is already consumed")
	}
	if err := repo.ConsumeToken(ctx, token.ID); err != nil {
		t.Fatalf("ConsumeToken: %v", err)
	}
	token, err = repo.TokenByHash(ctx, "hash-1", store.TokenKindAccess)
	if err != nil {
		t.Fatalf("TokenByHash after consume: %v", err)
	}
	if token.ConsumedAt == nil {
		t.Error("token was not marked consumed")
	}

	for _, connector := range []string{"metrika", "metrika", "webmaster"} {
		if err := repo.RecordCall(context.Background(), user.ID, connector, "get_counters", "ok"); err != nil {
			t.Fatalf("RecordCall: %v", err)
		}
	}
	usage, err := repo.Usage(ctx, user.ID, user.CreatedAt.Add(-1e9))
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if usage.CallsToday != 3 || usage.ByConnector["metrika"] != 2 {
		t.Errorf("usage = %+v", usage)
	}

	if err := repo.CreateLead(ctx, store.Lead{Email: "buyer@example.com", Service: "direct"}); err != nil {
		t.Fatalf("CreateLead: %v", err)
	}
	if err := repo.CreateLead(ctx, store.Lead{Email: "buyer@example.com", Service: "wordstat", UserID: user.ID}); err != nil {
		t.Fatalf("CreateLead with user: %v", err)
	}

	if err := repo.DeleteAccount(ctx, user.ID, "yandex"); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	if _, err := repo.AccountByUser(ctx, user.ID, "yandex"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("AccountByUser after delete = %v, want ErrNotFound", err)
	}
}

func TestPostgresStats(t *testing.T) {
	repo := testStore(t)
	ctx := context.Background()

	if err := migrations.Up(ctx, repo.Pool()); err != nil {
		t.Fatalf("migrations.Up: %v", err)
	}

	// Empty stats
	emptyStats, err := repo.Stats(ctx)
	if err != nil {
		t.Fatalf("repo.Stats on empty: %v", err)
	}
	if emptyStats.TotalCalls != 0 || len(emptyStats.Users) != 0 {
		t.Errorf("empty stats = %+v, want 0 calls and 0 users", emptyStats)
	}

	u1, err := repo.UpsertUser(ctx, store.User{YandexID: "y100", Login: "user1", Email: "u1@example.com", Name: "User 1"})
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	u2, err := repo.UpsertUser(ctx, store.User{YandexID: "y200", Login: "user2", Email: "u2@example.com", Name: "User 2"})
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	// u3 has no calls
	_, err = repo.UpsertUser(ctx, store.User{YandexID: "y300", Login: "user3", Email: "u3@example.com", Name: "User 3"})
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	if err := repo.RecordCall(ctx, u1.ID, "metrika", "get_counters", "ok"); err != nil {
		t.Fatalf("RecordCall: %v", err)
	}
	if err := repo.RecordCall(ctx, u1.ID, "metrika", "get_reports", "ok"); err != nil {
		t.Fatalf("RecordCall: %v", err)
	}
	if err := repo.RecordCall(ctx, u2.ID, "webmaster", "list_hosts", "ok"); err != nil {
		t.Fatalf("RecordCall: %v", err)
	}

	stats, err := repo.Stats(ctx)
	if err != nil {
		t.Fatalf("repo.Stats: %v", err)
	}

	if stats.TotalCalls != 3 {
		t.Errorf("TotalCalls = %d, want 3", stats.TotalCalls)
	}
	if len(stats.Users) != 2 {
		t.Fatalf("len(Users) = %d, want 2", len(stats.Users))
	}
	if stats.Users[0].UserID != u1.ID || stats.Users[0].Calls != 2 {
		t.Errorf("u1 stats mismatch: %+v", stats.Users[0])
	}
	if stats.Users[0].Login != "user1" || stats.Users[0].Email != "u1@example.com" {
		t.Errorf("u1 user data mismatch: %+v", stats.Users[0])
	}
	if stats.Users[1].UserID != u2.ID || stats.Users[1].Calls != 1 {
		t.Errorf("u2 stats mismatch: %+v", stats.Users[1])
	}
}

type fakeYandex struct{ token string }

func (f fakeYandex) Exchange(context.Context, string, string) (auth.YandexToken, error) {
	return auth.YandexToken{AccessToken: f.token, RefreshToken: "refresh", ExpiresIn: 3600, Scope: "metrika:read"}, nil
}

func (f fakeYandex) Profile(context.Context, string) (auth.YandexProfile, error) {
	return auth.YandexProfile{ID: "100500", Login: "egor", Email: "egor@example.com", Name: "Egor"}, nil
}
