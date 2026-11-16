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

// Daily call statuses written to call_log.
const (
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
			"metrika_bytime, metrika_goals and metrika_traffic_sources.",
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

	args, err := normalize(tool.schema, raw)
	if err != nil {
		b.record(ctx, userID, tool, statusError)
		return errorResult("Неверные аргументы: " + err.Error())
	}

	// The limit is checked against call_log and the call is written to it in
	// one step: two parallel calls of the same user must not both take the last
	// slot. Calls of different users do not block each other.
	unlock := b.lockUser(userID)
	defer unlock()

	usage, err := b.deps.Repo.Usage(ctx, userID, b.dayStart())
	if err != nil {
		b.logger().Error("mcp: usage", "error", err, "user", userID)
		b.record(ctx, userID, tool, statusError)
		return errorResult("Не удалось проверить дневной лимит, повторите запрос позже.")
	}
	limit := b.limit()
	if usage.CallsToday >= limit {
		b.record(ctx, userID, tool, statusLimit)
		return errorResult(fmt.Sprintf(
			"Дневной лимит исчерпан: %d из %d вызовов за сутки. Лимит обновится после полуночи UTC; в кабинете видно, сколько вызовов осталось.",
			usage.CallsToday, limit))
	}

	accessToken, err := b.accessToken(ctx, userID)
	switch {
	case errors.Is(err, ErrNoAccount):
		b.record(ctx, userID, tool, statusNoAccount)
		return errorResult("Аккаунт Яндекса не подключён. Войдите в кабинет " + b.cabinetURL() + ", подключите аккаунт Яндекса и повторите запрос.")
	case errors.Is(err, ErrReconnect):
		b.logger().Warn("mcp: yandex token", "error", err, "user", userID)
		b.record(ctx, userID, tool, statusError)
		return errorResult("Срок действия токена Яндекса истёк. Подключите аккаунт заново в кабинете " + b.cabinetURL() + ".")
	case err != nil:
		b.logger().Error("mcp: yandex token", "error", err, "user", userID)
		b.record(ctx, userID, tool, statusError)
		return errorResult("Не удалось получить токен Яндекса, повторите запрос позже.")
	}

	payload, err := tool.tool.Call(ctx, connectors.Env{AccessToken: accessToken}, args)
	if err != nil {
		b.record(ctx, userID, tool, statusError)
		return errorResult(err.Error())
	}
	b.record(ctx, userID, tool, statusOK)

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

// record writes the call to call_log. A journal failure is logged: the call
// itself already happened and the user should get its result.
func (b *Bridge) record(ctx context.Context, userID string, tool registered, status string) {
	if err := b.deps.Repo.RecordCall(ctx, userID, tool.connector, tool.tool.Name, status); err != nil {
		b.logger().Error("mcp: call log", "error", err, "user", userID, "connector", tool.connector, "tool", tool.tool.Name)
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
