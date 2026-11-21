package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/babang1da/rumcp/server/internal/config"
	"github.com/babang1da/rumcp/server/internal/store"
	"github.com/babang1da/rumcp/server/internal/store/storetest"
)

func newTestRouterWithToken(t *testing.T, token string, repo store.Repository) http.Handler {
	t.Helper()
	cfg := config.Config{
		BaseURL:       "http://localhost:8080",
		TokenKey:      make([]byte, 32),
		SessionSecret: []byte("0123456789abcdef"),
		DailyLimit:    100,
		StatsToken:    token,
	}
	router, err := NewRouter(cfg, repo)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}
	return router
}

func TestStatsEndpointAuth(t *testing.T) {
	const validToken = "my-secret-stats-token"
	repo := storetest.New()
	router := newTestRouterWithToken(t, validToken, repo)

	tests := []struct {
		name       string
		authHeader string
		wantCode   int
	}{
		{
			name:       "missing auth header",
			authHeader: "",
			wantCode:   http.StatusUnauthorized,
		},
		{
			name:       "non-bearer scheme",
			authHeader: "Basic dXNlcjpwYXNz",
			wantCode:   http.StatusUnauthorized,
		},
		{
			name:       "wrong bearer token",
			authHeader: "Bearer wrong-token",
			wantCode:   http.StatusUnauthorized,
		},
		{
			name:       "empty bearer token",
			authHeader: "Bearer ",
			wantCode:   http.StatusUnauthorized,
		},
		{
			name:       "valid bearer token",
			authHeader: "Bearer " + validToken,
			wantCode:   http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantCode, rec.Body.String())
			}

			if tc.wantCode == http.StatusUnauthorized {
				var errResp map[string]string
				if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
					t.Fatalf("decode error response: %v", err)
				}
				if errResp["error"] != "unauthorized" {
					t.Errorf("error = %q, want %q", errResp["error"], "unauthorized")
				}
			}
		})
	}
}

func TestStatsEndpointEmptyTokenRejectsAll(t *testing.T) {
	repo := storetest.New()
	// StatsToken is empty: endpoint must be closed
	router := newTestRouterWithToken(t, "", repo)

	for _, header := range []string{"", "Bearer ", "Bearer test", "Bearer anything"} {
		req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("with header %q: status = %d, want %d", header, rec.Code, http.StatusUnauthorized)
		}
	}
}

func TestStatsEndpointMethodNotAllowed(t *testing.T) {
	repo := storetest.New()
	router := newTestRouterWithToken(t, "token", repo)

	req := httptest.NewRequest(http.MethodPost, "/api/stats", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/stats status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestStatsEndpointContent(t *testing.T) {
	const validToken = "stats-token-xyz"
	repo := storetest.New()
	ctx := context.Background()

	// Create 3 users
	u1, err := repo.UpsertUser(ctx, store.User{YandexID: "y1", Login: "user1", Email: "u1@example.com", Name: "User 1"})
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	u2, err := repo.UpsertUser(ctx, store.User{YandexID: "y2", Login: "user2", Email: "u2@example.com", Name: "User 2"})
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	_, err = repo.UpsertUser(ctx, store.User{YandexID: "y3", Login: "user3", Email: "u3@example.com", Name: "User 3"})
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	// u1 makes 2 calls, u2 makes 1 call, u3 makes 0 calls
	if err := repo.RecordCall(ctx, u1.ID, "metrika", "get_counters", "ok"); err != nil {
		t.Fatalf("RecordCall: %v", err)
	}
	if err := repo.RecordCall(ctx, u1.ID, "metrika", "get_reports", "ok"); err != nil {
		t.Fatalf("RecordCall: %v", err)
	}
	if err := repo.RecordCall(ctx, u2.ID, "webmaster", "list_hosts", "ok"); err != nil {
		t.Fatalf("RecordCall: %v", err)
	}

	router := newTestRouterWithToken(t, validToken, repo)

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	req.Header.Set("Authorization", "Bearer "+validToken)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json; charset=utf-8", ct)
	}

	var resp struct {
		TotalCalls  int64 `json:"total_calls"`
		Users       []store.UserStats
		GeneratedAt time.Time `json:"generated_at"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if resp.TotalCalls != 3 {
		t.Errorf("TotalCalls = %d, want 3", resp.TotalCalls)
	}
	// u3 made 0 calls, so len(Users) must be 2
	if len(resp.Users) != 2 {
		t.Fatalf("len(Users) = %d, want 2 (only users with calls)", len(resp.Users))
	}

	// Verify user 1
	var foundU1, foundU2 bool
	for _, u := range resp.Users {
		if u.UserID == u1.ID {
			foundU1 = true
			if u.Login != "user1" || u.Email != "u1@example.com" {
				t.Errorf("u1 data mismatch: %+v", u)
			}
			if u.Calls != 2 {
				t.Errorf("u1 calls = %d, want 2", u.Calls)
			}
			if u.FirstCallAt.IsZero() || u.LastCallAt.IsZero() {
				t.Errorf("u1 zero timestamp: first=%v, last=%v", u.FirstCallAt, u.LastCallAt)
			}
		}
		if u.UserID == u2.ID {
			foundU2 = true
			if u.Login != "user2" || u.Email != "u2@example.com" {
				t.Errorf("u2 data mismatch: %+v", u)
			}
			if u.Calls != 1 {
				t.Errorf("u2 calls = %d, want 1", u.Calls)
			}
		}
	}
	if !foundU1 || !foundU2 {
		t.Errorf("foundU1 = %v, foundU2 = %v, want both true", foundU1, foundU2)
	}

	if resp.GeneratedAt.IsZero() {
		t.Error("GeneratedAt is zero")
	}
	if time.Since(resp.GeneratedAt) > 10*time.Second {
		t.Errorf("GeneratedAt is too old: %v", resp.GeneratedAt)
	}
}

func TestStatsEndpointEmptyDatabase(t *testing.T) {
	const validToken = "stats-token-empty"
	repo := storetest.New()
	router := newTestRouterWithToken(t, validToken, repo)

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	req.Header.Set("Authorization", "Bearer "+validToken)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	var raw map[string]any
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}

	if raw["total_calls"] != float64(0) {
		t.Errorf("total_calls = %v, want 0", raw["total_calls"])
	}
	usersRaw, ok := raw["users"].([]any)
	if !ok || len(usersRaw) != 0 {
		t.Errorf("users = %v, want empty array []", raw["users"])
	}
	if raw["generated_at"] == nil {
		t.Error("generated_at is missing")
	}
}

type errStatsRepo struct {
	store.Repository
}

func (e *errStatsRepo) Stats(_ context.Context) (store.Stats, error) {
	return store.Stats{}, errors.New("db error")
}

func TestStatsEndpointDatabaseError(t *testing.T) {
	repo := &errStatsRepo{Repository: storetest.New()}
	router := newTestRouterWithToken(t, "token", repo)

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	var errResp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp["error"] != "stats_failed" {
		t.Errorf("error = %q, want stats_failed", errResp["error"])
	}
}
