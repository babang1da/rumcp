package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/babang1da/rumcp/server/internal/connectors"
	"github.com/babang1da/rumcp/server/internal/connectors/metrika"
	"github.com/babang1da/rumcp/server/internal/mcp"
	"github.com/babang1da/rumcp/server/internal/oauth"
	"github.com/babang1da/rumcp/server/internal/secret"
	"github.com/babang1da/rumcp/server/internal/store"
	"github.com/babang1da/rumcp/server/internal/store/storetest"
)

const (
	accessToken  = "rumcp-access-token"
	yandexToken  = "yandex-access"
	yandexToken2 = "yandex-access-refreshed"
)

// call is one row the bridge wrote to call_log.
type call struct {
	userID    string
	connector string
	tool      string
	status    string
}

// journal wraps the in-memory store and keeps the calls for assertions.
type journal struct {
	*storetest.Memory
	mu    sync.Mutex
	calls []call
}

func (j *journal) RecordCall(ctx context.Context, userID, connector, tool, status string) error {
	j.mu.Lock()
	j.calls = append(j.calls, call{userID: userID, connector: connector, tool: tool, status: status})
	j.mu.Unlock()
	return j.Memory.RecordCall(ctx, userID, connector, tool, status)
}

func (j *journal) all() []call {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]call(nil), j.calls...)
}

// metrikaStub is a fake Metrica API that records the last token it was called with.
type metrikaStub struct {
	server *httptest.Server
	mu     sync.Mutex
	path   string
	query  string
	token  string
	body   string
	status int
}

func newMetrikaStub(t *testing.T) *metrikaStub {
	t.Helper()
	s := &metrikaStub{
		body:   `{"counters":[{"id":123456,"name":"Мой сайт","site":"example.com","status":"Active"}],"total_rows":1,"data":[]}`,
		status: http.StatusOK,
	}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.path = r.URL.Path
		s.query = r.URL.RawQuery
		s.token = strings.TrimPrefix(r.Header.Get("Authorization"), "OAuth ")
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s.status)
		_, _ = w.Write([]byte(s.body))
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *metrikaStub) respond(status int, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
	s.body = body
}

func (s *metrikaStub) last() (path, query, token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.path, s.query, s.token
}

// env is one ready-to-use service: a journal, a user with a Yandex account, a
// Bearer token and the /mcp route guarded by the OAuth server.
type env struct {
	t        *testing.T
	repo     *journal
	box      *secret.Box
	userID   string
	server   *httptest.Server
	metrika  *metrikaStub
	endpoint string
}

type envConfig struct {
	limit          int
	withoutAccount bool
	expired        bool
	noRefresh      bool
	refresher      mcp.TokenRefresher
}

type option func(*envConfig)

func withLimit(limit int) option { return func(c *envConfig) { c.limit = limit } }
func withoutAccount() option     { return func(c *envConfig) { c.withoutAccount = true } }
func expiredAccount() option     { return func(c *envConfig) { c.expired = true } }
func expiredWithoutRefresh() option {
	return func(c *envConfig) { c.expired, c.noRefresh = true, true }
}
func withRefresher(r mcp.TokenRefresher) option { return func(c *envConfig) { c.refresher = r } }

func newEnv(t *testing.T, opts ...option) *env {
	t.Helper()
	cfg := envConfig{limit: 100}
	for _, opt := range opts {
		opt(&cfg)
	}

	ctx := context.Background()
	mem := storetest.New()
	repo := &journal{Memory: mem}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	box, err := secret.NewBox(key)
	if err != nil {
		t.Fatalf("secret.NewBox: %v", err)
	}

	user, err := mem.UpsertUser(ctx, store.User{YandexID: "100500", Login: "egor", Email: "egor@example.com"})
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	if !cfg.withoutAccount {
		sealedAccess, err := box.Seal(yandexToken)
		if err != nil {
			t.Fatalf("seal access: %v", err)
		}
		sealedRefresh, err := box.Seal("yandex-refresh")
		if err != nil {
			t.Fatalf("seal refresh: %v", err)
		}
		expires := time.Now().Add(time.Hour)
		if cfg.expired {
			expires = time.Now().Add(-time.Hour)
		}
		account := store.Account{
			UserID:          user.ID,
			Provider:        "yandex",
			AccessTokenEnc:  sealedAccess,
			RefreshTokenEnc: sealedRefresh,
			Scope:           "metrika:read",
			ExpiresAt:       &expires,
		}
		if cfg.noRefresh {
			account.RefreshTokenEnc = ""
		}
		if err := mem.SaveAccount(ctx, account); err != nil {
			t.Fatalf("SaveAccount: %v", err)
		}
	}

	if err := mem.CreateToken(ctx, store.OAuthToken{
		UserID:    user.ID,
		ClientID:  "test-client",
		Kind:      store.TokenKindAccess,
		TokenHash: hashToken(accessToken),
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	stub := newMetrikaStub(t)
	connector := metrika.New()
	connector.BaseURL = stub.server.URL

	bridge, err := mcp.New(mcp.Deps{
		Repo:       repo,
		Box:        box,
		Connectors: []connectors.Connector{connector},
		DailyLimit: cfg.limit,
		Yandex:     cfg.refresher,
		BaseURL:    "http://localhost:3000",
	})
	if err != nil {
		t.Fatalf("mcp.New: %v", err)
	}

	oauthHandler := &oauth.Handler{Repo: mem, Cfg: oauth.DefaultConfig("http://localhost:8080")}
	mux := http.NewServeMux()
	mux.Handle("/mcp", bridge.Handler(oauthHandler))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &env{
		t:        t,
		repo:     repo,
		box:      box,
		userID:   user.ID,
		server:   srv,
		metrika:  stub,
		endpoint: srv.URL + "/mcp",
	}
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// bearerTransport adds the service Bearer token to every MCP request.
type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (b *bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	clone.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(clone)
}

func connect(t *testing.T, endpoint, token string) *sdk.ClientSession {
	t.Helper()
	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "1.0"}, nil)
	httpClient := &http.Client{Transport: &bearerTransport{token: token, base: http.DefaultTransport}}
	session, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{
		Endpoint:             endpoint,
		HTTPClient:           httpClient,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func callTool(t *testing.T, session *sdk.ClientSession, name string, args map[string]any) *sdk.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return result
}

func text(t *testing.T, result *sdk.CallToolResult) string {
	t.Helper()
	var out strings.Builder
	for _, content := range result.Content {
		if item, ok := content.(*sdk.TextContent); ok {
			out.WriteString(item.Text)
		}
	}
	return out.String()
}

// TestToolsListAndCall covers initialize, tools/list and a successful tools/call.
func TestToolsListAndCall(t *testing.T) {
	e := newEnv(t)
	session := connect(t, e.endpoint, accessToken)

	want := map[string]bool{
		"metrika_counters":        false,
		"metrika_table":           false,
		"metrika_bytime":          false,
		"metrika_goals":           false,
		"metrika_traffic_sources": false,
	}
	for tool, err := range session.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatalf("tools/list: %v", err)
		}
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
		if tool.Description == "" {
			t.Errorf("tool %s: empty description", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %s: no input schema", tool.Name)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("tools/list misses %s", name)
		}
	}

	result := callTool(t, session, "metrika_counters", map[string]any{})
	if result.IsError {
		t.Fatalf("call failed: %s", text(t, result))
	}
	if body := text(t, result); !strings.Contains(body, "123456") || !strings.Contains(body, "Мой сайт") {
		t.Errorf("result: %s", body)
	}

	path, _, token := e.metrika.last()
	if path != "/management/v1/counters" {
		t.Errorf("metrika path: %s", path)
	}
	if token != yandexToken {
		t.Errorf("metrika token: %q", token)
	}

	calls := e.repo.all()
	if len(calls) != 1 {
		t.Fatalf("call_log: want 1 row, got %d", len(calls))
	}
	if calls[0].connector != "metrika" || calls[0].tool != "counters" || calls[0].status != "ok" || calls[0].userID != e.userID {
		t.Errorf("call_log row: %+v", calls[0])
	}
}

// TestDailyLimit covers the daily cap: the 101st call of the day is rejected
// and the rejection is journalled too.
func TestDailyLimit(t *testing.T) {
	e := newEnv(t)
	session := connect(t, e.endpoint, accessToken)

	for i := 1; i <= 100; i++ {
		result := callTool(t, session, "metrika_counters", map[string]any{})
		if result.IsError {
			t.Fatalf("call %d failed: %s", i, text(t, result))
		}
	}

	result := callTool(t, session, "metrika_counters", map[string]any{})
	if !result.IsError {
		t.Fatal("101st call must be rejected")
	}
	if body := text(t, result); !strings.Contains(body, "Дневной лимит исчерпан") {
		t.Errorf("101st call message: %s", body)
	}

	calls := e.repo.all()
	if len(calls) != 101 {
		t.Fatalf("call_log: want 101 rows, got %d", len(calls))
	}
	if calls[100].status != "limit" {
		t.Errorf("101st status: %q", calls[100].status)
	}
	okCalls := 0
	for _, item := range calls {
		if item.status == "ok" {
			okCalls++
		}
	}
	if okCalls != 100 {
		t.Errorf("ok calls: want 100, got %d", okCalls)
	}
}

// TestBearerRequired covers the unauthenticated request.
func TestBearerRequired(t *testing.T) {
	e := newEnv(t)
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`

	for name, token := range map[string]string{"missing": "", "unknown": "not-a-token"} {
		t.Run(name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, e.endpoint, strings.NewReader(body))
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status: want 401, got %d", resp.StatusCode)
			}
			challenge := resp.Header.Get("WWW-Authenticate")
			if !strings.Contains(challenge, "Bearer") {
				t.Errorf("WWW-Authenticate: %q", challenge)
			}
			if !strings.Contains(challenge, "resource_metadata") {
				t.Errorf("WWW-Authenticate: %q", challenge)
			}
			payload, _ := io.ReadAll(resp.Body)
			if !strings.Contains(string(payload), "invalid_token") {
				t.Errorf("body: %s", payload)
			}
		})
	}
}

// TestNoAccount covers a user who has not connected Yandex yet.
func TestNoAccount(t *testing.T) {
	e := newEnv(t, withoutAccount())
	session := connect(t, e.endpoint, accessToken)

	result := callTool(t, session, "metrika_counters", map[string]any{})
	if !result.IsError {
		t.Fatal("call must fail without a connected account")
	}
	if body := text(t, result); !strings.Contains(body, "не подключён") {
		t.Errorf("message: %s", body)
	}
	calls := e.repo.all()
	if len(calls) != 1 || calls[0].status != "no_account" {
		t.Fatalf("call_log: %+v", calls)
	}
}

// TestTokenRefresh covers an expired Yandex token: the bridge refreshes it,
// stores the new one sealed and calls Metrica with it.
func TestTokenRefresh(t *testing.T) {
	var mu sync.Mutex
	var form map[string]string
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		mu.Lock()
		form = map[string]string{
			"grant_type":    r.Form.Get("grant_type"),
			"refresh_token": r.Form.Get("refresh_token"),
			"client_id":     r.Form.Get("client_id"),
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  yandexToken2,
			"refresh_token": "yandex-refresh-2",
			"expires_in":    3600,
		})
	}))
	defer tokenServer.Close()

	e := newEnv(t, expiredAccount(), withRefresher(&mcp.YandexRefresher{
		ClientID:     "app-id",
		ClientSecret: "app-secret",
		TokenURL:     tokenServer.URL,
		Client:       tokenServer.Client(),
	}))
	session := connect(t, e.endpoint, accessToken)

	result := callTool(t, session, "metrika_counters", map[string]any{})
	if result.IsError {
		t.Fatalf("call failed: %s", text(t, result))
	}

	mu.Lock()
	got := form
	mu.Unlock()
	if got["grant_type"] != "refresh_token" || got["refresh_token"] != "yandex-refresh" || got["client_id"] != "app-id" {
		t.Errorf("refresh form: %v", got)
	}
	if _, _, token := e.metrika.last(); token != yandexToken2 {
		t.Errorf("metrica token: %q", token)
	}

	account, err := e.repo.AccountByUser(context.Background(), e.userID, "yandex")
	if err != nil {
		t.Fatalf("AccountByUser: %v", err)
	}
	stored, err := e.box.Open(account.AccessTokenEnc)
	if err != nil {
		t.Fatalf("open stored token: %v", err)
	}
	if stored != yandexToken2 {
		t.Errorf("stored token: %q", stored)
	}
	if account.ExpiresAt == nil || !account.ExpiresAt.After(time.Now()) {
		t.Errorf("expiry not extended: %v", account.ExpiresAt)
	}
	if calls := e.repo.all(); len(calls) != 1 || calls[0].status != "ok" {
		t.Errorf("call_log: %+v", calls)
	}
}

// TestExpiredWithoutRefresh covers an expired token that cannot be refreshed.
func TestExpiredWithoutRefresh(t *testing.T) {
	e := newEnv(t, expiredWithoutRefresh())
	session := connect(t, e.endpoint, accessToken)

	result := callTool(t, session, "metrika_counters", map[string]any{})
	if !result.IsError {
		t.Fatal("call must fail")
	}
	if body := text(t, result); !strings.Contains(body, "истёк") {
		t.Errorf("message: %s", body)
	}
	if calls := e.repo.all(); len(calls) != 1 || calls[0].status != "error" {
		t.Errorf("call_log: %+v", calls)
	}
}

// TestInvalidArguments covers argument validation before any Metrica request.
func TestInvalidArguments(t *testing.T) {
	e := newEnv(t)
	session := connect(t, e.endpoint, accessToken)

	result := callTool(t, session, "metrika_table", map[string]any{})
	if !result.IsError {
		t.Fatal("call without counter_id must fail")
	}
	if body := text(t, result); !strings.Contains(body, "counter_id") {
		t.Errorf("message: %s", body)
	}
	if path, _, _ := e.metrika.last(); path != "" {
		t.Errorf("metrika must not be called, got %s", path)
	}
	calls := e.repo.all()
	if len(calls) != 1 || calls[0].status != "error" || calls[0].tool != "table" {
		t.Errorf("call_log: %+v", calls)
	}
}

// TestSchemaDefaults covers the defaults declared in the tool schema: they reach
// the connector without the client sending them.
func TestSchemaDefaults(t *testing.T) {
	e := newEnv(t)
	session := connect(t, e.endpoint, accessToken)

	result := callTool(t, session, "metrika_table", map[string]any{"counter_id": 123456})
	if result.IsError {
		t.Fatalf("call failed: %s", text(t, result))
	}
	_, query, _ := e.metrika.last()
	for _, wantPart := range []string{"ids=123456", "date1=7daysAgo", "date2=today", "limit=100", "metrics=ym%3As%3Avisits%2Cym%3As%3Ausers"} {
		if !strings.Contains(query, wantPart) {
			t.Errorf("query %q misses %q", query, wantPart)
		}
	}
}

// TestMetrikaError covers a failed Metrica response: the assistant gets the
// API message and the call is journalled as an error.
func TestMetrikaError(t *testing.T) {
	e := newEnv(t)
	e.metrika.respond(http.StatusForbidden, `{"errors":[{"error_type":"invalid_token","message":"Invalid oauth_token"}]}`)
	session := connect(t, e.endpoint, accessToken)

	result := callTool(t, session, "metrika_counters", map[string]any{})
	if !result.IsError {
		t.Fatal("call must fail on a Metrica error")
	}
	if body := text(t, result); !strings.Contains(body, "Invalid oauth_token") {
		t.Errorf("message: %s", body)
	}
	calls := e.repo.all()
	if len(calls) != 1 || calls[0].status != "error" || calls[0].connector != "metrika" {
		t.Errorf("call_log: %+v", calls)
	}
}
