package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/babang1da/rumcp/server/internal/config"
	"github.com/babang1da/rumcp/server/internal/store/storetest"
)

// testRouter собирает маршруты на пустом in-memory хранилище.
func testRouter(t *testing.T) http.Handler {
	t.Helper()
	key := make([]byte, 32)
	cfg := config.Config{
		BaseURL:       "http://localhost:8080",
		TokenKey:      key,
		SessionSecret: []byte("0123456789abcdef"),
		DailyLimit:    100,
	}
	router, err := NewRouter(cfg, storetest.New())
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	return router
}

func TestHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	testRouter(t).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}

	var body struct {
		Status  string `json:"status"`
		Service string `json:"service"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "ok" || body.Service != "server" {
		t.Errorf("body = %+v, want status=ok service=server", body)
	}
}

func TestHealthzRejectsOtherMethods(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	rec := httptest.NewRecorder()

	testRouter(t).ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

// TestMCPRequiresBearer проверяет, что /mcp без токена сервиса получает 401 и
// WWW-Authenticate с адресом метаданных — по нему MCP-клиент находит OAuth.
func TestMCPRequiresBearer(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rec := httptest.NewRecorder()

	testRouter(t).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	challenge := rec.Header().Get("WWW-Authenticate")
	if !strings.Contains(challenge, "Bearer") || !strings.Contains(challenge, "resource_metadata") {
		t.Errorf("WWW-Authenticate = %q", challenge)
	}
}
