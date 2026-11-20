// Package mcp serves the remote MCP endpoint /mcp on Streamable HTTP with the
// official Go SDK.
//
// Every request must carry a Bearer token of the service. A tool call is
// served in four steps: the arguments are checked against the tool schema, the
// daily limit of the user is checked, the Yandex token of the user is resolved
// (and refreshed when it expired), and the call is written to call_log with its
// status — successful calls and failed ones alike, which is what the product
// metric counts.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/babang1da/rumcp/server/internal/connectors"
	"github.com/babang1da/rumcp/server/internal/oauth"
	"github.com/babang1da/rumcp/server/internal/secret"
	"github.com/babang1da/rumcp/server/internal/store"
)

// Daily call statuses written to call_log. A call opens its row with
// statusPending and gets the final status when it ends.
const (
	statusPending   = "pending"
	statusOK        = "ok"
	statusError     = "error"
	statusLimit     = "limit"
	statusNoAccount = "no_account"
)

// providerYandex is the connected account the tools read.
const providerYandex = "yandex"

// defaultLimit is the calls-per-user-per-day cap when none is configured.
const defaultLimit = 100

// version is the MCP server version reported to clients.
const version = "0.1.0"

// Repository is the data access the bridge needs.
type Repository interface {
	AccountByUser(ctx context.Context, userID, provider string) (store.Account, error)
	SaveAccount(ctx context.Context, a store.Account) error
	RecordCall(ctx context.Context, userID, connector, tool, status string) error
	StartCall(ctx context.Context, userID, connector, tool, status string) (int64, error)
	FinishCall(ctx context.Context, id int64, status string) error
	Usage(ctx context.Context, userID string, since time.Time) (store.Usage, error)
}

// Deps are the collaborators of the bridge.
type Deps struct {
	Repo       Repository
	Box        *secret.Box
	Connectors []connectors.Connector
	// DailyLimit is the number of tool calls per user per day.
	DailyLimit int
	// Yandex refreshes expired Yandex tokens; nil means the user has to
	// reconnect the account once the token expires.
	Yandex TokenRefresher
	// BaseURL is the public origin, used in messages that point to the cabinet.
	BaseURL string
	// Now is the clock; tests replace it.
	Now func() time.Time
	// Logger receives bridge errors; nil means slog.Default.
	Logger *slog.Logger
}

// Bridge exposes connectors as MCP tools.
type Bridge struct {
	deps  Deps
	tools []registered

	// limits guards locks: the daily limit is checked and journalled in one
	// step per user, so parallel calls of one user cannot overshoot it.
	limits sync.Mutex
	locks  map[string]*userLock
}

// userLock serializes the tool calls of one user.
type userLock struct {
	mu   sync.Mutex
	refs int
}

// registered is one tool of one connector, ready for /mcp.
type registered struct {
	connector string
	tool      connectors.Tool
	name      string
	schema    *jsonschema.Resolved
}

// New builds the bridge and validates the tool schemas.
func New(deps Deps) (*Bridge, error) {
	if deps.Repo == nil {
		return nil, errors.New("mcp: repository is required")
	}
	if deps.Box == nil {
		return nil, errors.New("mcp: secret box is required")
	}
	if deps.DailyLimit <= 0 {
		deps.DailyLimit = defaultLimit
	}

	b := &Bridge{deps: deps, locks: map[string]*userLock{}}
	seen := map[string]bool{}
	for _, connector := range deps.Connectors {
		for _, tool := range connector.Tools() {
			name := connector.Name() + "_" + tool.Name
			if seen[name] {
				return nil, fmt.Errorf("mcp: duplicate tool %q", name)
			}
			seen[name] = true
			schema, err := resolveSchema(tool.InputSchema)
			if err != nil {
				return nil, fmt.Errorf("mcp: tool %q: %w", name, err)
			}
			b.tools = append(b.tools, registered{connector: connector.Name(), tool: tool, name: name, schema: schema})
		}
	}
	return b, nil
}

// Handler returns /mcp: requests without a valid Bearer token are rejected by
// the OAuth server with 401 and WWW-Authenticate.
func (b *Bridge) Handler(auth *oauth.Handler) http.Handler {
	handler := sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		userID, _ := oauth.UserIDFromContext(r.Context())
		return b.server(userID)
	}, &sdk.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
		Logger:       b.deps.Logger,
	})
	return auth.RequireBearer(handler)
}

// server builds the MCP server of one request. The tools are the same for
// everybody, the user behind the Bearer token is captured by the handlers.
func (b *Bridge) server(userID string) *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "rumcp", Title: "RuMCP", Version: version}, &sdk.ServerOptions{
		Logger: b.deps.Logger,
		Instructions: "RuMCP gives read-only access to the Yandex marketing stack of the signed-in user. " +
			"Start with metrika_counters to learn counter ids, then run reports with metrika_table, " +
			"metrika_bytime, metrika_goals and metrika_traffic_sources. " +
			"For Yandex Webmaster start with webmaster_sites to learn site ids, then use " +
			"webmaster_indexing, webmaster_search_queries, webmaster_diagnostics and webmaster_sitemaps.",
	})

	for _, tool := range b.tools {
		t := tool
		s.AddTool(&sdk.Tool{
			Name:        t.name,
			Description: t.tool.Description,
			InputSchema: t.tool.InputSchema,
			Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
		}, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			return b.call(ctx, userID, t, req.Params.Arguments), nil
		})
	}
	return s
}

// call runs one tool call and journals it. Failures are tool errors, not
// protocol errors, so the assistant sees them and can correct itself.
func (b *Bridge) call(ctx context.Context, userID string, tool registered, raw json.RawMessage) *sdk.CallToolResult {
	if userID == "" {
		return errorResult("Нет активной сессии. Подключите RuMCP заново.")
	}

	// The limit, the journal row of this call and the Yandex token are handled
	// under the lock of the user: the row opened here is what the next call
	// counts, so parallel calls cannot overshoot the limit, and a token refresh
	// runs once. The connector call itself runs outside the lock, so one user's
	// calls do not queue behind a slow Yandex response.
	unlock := b.lockUser(userID)

	usage, err := b.deps.Repo.Usage(ctx, userID, b.dayStart())
	if err != nil {
		b.logger().Error("mcp: usage", "error", err, "user", userID)
		b.record(ctx, userID, tool, statusError)
		unlock()
		return errorResult("Не удалось проверить дневной лимит, повторите запрос позже.")
	}
	limit := b.limit()
	if usage.CallsToday >= limit {
		b.record(ctx, userID, tool, statusLimit)
		unlock()
		return errorResult(fmt.Sprintf(
			"Дневной лимит исчерпан: %d вызовов в сутки на пользователя. Лимит обновится после полуночи UTC; сколько вызовов осталось, видно в кабинете.",
			limit))
	}

	accessToken, err := b.accessToken(ctx, userID)
	if err != nil {
		status := statusError
		if errors.Is(err, ErrNoAccount) {
			status = statusNoAccount
		}
		b.record(ctx, userID, tool, status)
		unlock()

		switch {
		case errors.Is(err, ErrNoAccount):
			return errorResult("Аккаунт Яндекса не подключён. Войдите в кабинет " + b.cabinetURL() + ", подключите аккаунт Яндекса и повторите запрос.")
		case errors.Is(err, ErrReconnect):
			b.logger().Warn("mcp: yandex token", "error", err, "user", userID)
			return errorResult("Срок действия токена Яндекса истёк. Подключите аккаунт заново в кабинете " + b.cabinetURL() + ".")
		default:
			b.logger().Error("mcp: yandex token", "error", err, "user", userID)
			return errorResult("Не удалось получить токен Яндекса, повторите запрос позже.")
		}
	}

	callID, err := b.deps.Repo.StartCall(ctx, userID, tool.connector, tool.tool.Name, statusPending)
	if err != nil {
		b.logger().Error("mcp: start call", "error", err, "user", userID)
		unlock()
		return errorResult("Не удалось записать вызов, повторите запрос позже.")
	}
	unlock()

	args, err := normalize(tool.schema, raw)
	if err != nil {
		b.finish(ctx, callID, statusError)
		return errorResult("Неверные аргументы: " + err.Error())
	}

	payload, err := tool.tool.Call(ctx, connectors.Env{AccessToken: accessToken}, args)
	if err != nil {
		b.finish(ctx, callID, statusError)
		return errorResult(err.Error())
	}
	b.finish(ctx, callID, statusOK)

	text, err := json.Marshal(payload)
	if err != nil {
		return errorResult("Не удалось подготовить ответ: " + err.Error())
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(text)}}}
}

// normalize applies the schema defaults and validates the arguments.
func normalize(schema *jsonschema.Resolved, raw json.RawMessage) (json.RawMessage, error) {
	args := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
	}
	if err := schema.ApplyDefaults(&args); err != nil {
		return nil, err
	}
	if err := schema.Validate(args); err != nil {
		return nil, err
	}
	return json.Marshal(args)
}

// record writes a finished call to call_log. A journal failure is logged: the
// call itself already happened and the user should get its result. The row is
// written even when the client cancelled the request — the journal is the
// product metric, and there is nothing to cancel in a finished insert.
func (b *Bridge) record(ctx context.Context, userID string, tool registered, status string) {
	if err := b.deps.Repo.RecordCall(context.WithoutCancel(ctx), userID, tool.connector, tool.tool.Name, status); err != nil {
		b.logger().Error("mcp: call log", "error", err, "user", userID, "connector", tool.connector, "tool", tool.tool.Name)
	}
}

// finish closes the row opened by StartCall with the final status.
func (b *Bridge) finish(ctx context.Context, callID int64, status string) {
	if err := b.deps.Repo.FinishCall(context.WithoutCancel(ctx), callID, status); err != nil {
		b.logger().Error("mcp: finish call", "error", err, "id", callID, "status", status)
	}
}

func (b *Bridge) limit() int {
	if b.deps.DailyLimit <= 0 {
		return defaultLimit
	}
	return b.deps.DailyLimit
}

// lockUser serializes the tool calls of one user and returns the release
// function. Locks are dropped when nobody holds them, so the map stays small.
func (b *Bridge) lockUser(userID string) func() {
	b.limits.Lock()
	lock := b.locks[userID]
	if lock == nil {
		lock = &userLock{}
		b.locks[userID] = lock
	}
	lock.refs++
	b.limits.Unlock()

	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		b.limits.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(b.locks, userID)
		}
		b.limits.Unlock()
	}
}

// dayStart is the beginning of the current UTC day, the window of the limit.
func (b *Bridge) dayStart() time.Time {
	now := b.now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

func (b *Bridge) now() time.Time {
	if b.deps.Now != nil {
		return b.deps.Now()
	}
	return time.Now()
}

func (b *Bridge) logger() *slog.Logger {
	if b.deps.Logger != nil {
		return b.deps.Logger
	}
	return slog.Default()
}

// cabinetURL points the user at the cabinet install instructions.
func (b *Bridge) cabinetURL() string {
	if b.deps.BaseURL == "" {
		return "/cabinet"
	}
	return b.deps.BaseURL + "/cabinet"
}

func errorResult(text string) *sdk.CallToolResult {
	return &sdk.CallToolResult{
		Content: []sdk.Content{&sdk.TextContent{Text: text}},
		IsError: true,
	}
}

// resolveSchema turns a JSON Schema literal into a validator.
func resolveSchema(schema map[string]any) (*jsonschema.Resolved, error) {
	if schema == nil {
		return nil, errors.New("missing input schema")
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	var parsed jsonschema.Schema
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	return parsed.Resolve(nil)
}
