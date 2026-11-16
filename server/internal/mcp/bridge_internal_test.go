package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/babang1da/rumcp/server/internal/connectors"
	"github.com/babang1da/rumcp/server/internal/secret"
	"github.com/babang1da/rumcp/server/internal/store"
	"github.com/babang1da/rumcp/server/internal/store/storetest"
)

// ctxGuard is the in-memory store that notices a journal write made with a
// cancelled context.
type ctxGuard struct {
	*storetest.Memory
	cancelled int
	statuses  []string
}

func (g *ctxGuard) observe(ctx context.Context, status string) {
	if ctx.Err() != nil {
		g.cancelled++
	}
	if status != "" {
		g.statuses = append(g.statuses, status)
	}
}

func (g *ctxGuard) RecordCall(ctx context.Context, userID, connector, tool, status string) error {
	g.observe(ctx, status)
	return g.Memory.RecordCall(ctx, userID, connector, tool, status)
}

func (g *ctxGuard) StartCall(ctx context.Context, userID, connector, tool, status string) (int64, error) {
	g.observe(ctx, "")
	return g.Memory.StartCall(ctx, userID, connector, tool, status)
}

func (g *ctxGuard) FinishCall(ctx context.Context, id int64, status string) error {
	g.observe(ctx, status)
	return g.Memory.FinishCall(ctx, id, status)
}

// cancelConnector cancels the request context while the tool runs, like a
// client that dropped the connection.
type cancelConnector struct {
	cancel context.CancelFunc
}

func (c cancelConnector) Name() string { return "stub" }

func (c cancelConnector) Tools() []connectors.Tool {
	return []connectors.Tool{{
		Name:        "call",
		Description: "stub",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Call: func(_ context.Context, _ connectors.Env, _ json.RawMessage) (any, error) {
			c.cancel()
			return map[string]any{"ok": true}, nil
		},
	}}
}

// TestCallJournalsCancelledRequest covers the product metric: a call whose
// client went away still gets its call_log row with the final status.
func TestCallJournalsCancelledRequest(t *testing.T) {
	ctx := context.Background()
	mem := storetest.New()
	guard := &ctxGuard{Memory: mem}

	box, err := secret.NewBox(make([]byte, 32))
	if err != nil {
		t.Fatalf("secret.NewBox: %v", err)
	}
	user, err := mem.UpsertUser(ctx, store.User{YandexID: "100500"})
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	sealed, err := box.Seal("yandex-token")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	expires := time.Now().Add(time.Hour)
	if err := mem.SaveAccount(ctx, store.Account{
		UserID:         user.ID,
		Provider:       "yandex",
		AccessTokenEnc: sealed,
		ExpiresAt:      &expires,
	}); err != nil {
		t.Fatalf("SaveAccount: %v", err)
	}

	callCtx, cancel := context.WithCancel(ctx)
	bridge, err := New(Deps{
		Repo:       guard,
		Box:        box,
		Connectors: []connectors.Connector{cancelConnector{cancel: cancel}},
		DailyLimit: 100,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	result := bridge.call(callCtx, user.ID, bridge.tools[0], nil)
	if result.IsError {
		t.Fatalf("call failed: %v", result.Content)
	}
	if guard.cancelled != 0 {
		t.Errorf("call_log written with a cancelled context %d times", guard.cancelled)
	}
	if len(guard.statuses) != 1 || guard.statuses[0] != statusOK {
		t.Errorf("statuses = %v, want [%s]", guard.statuses, statusOK)
	}
}
