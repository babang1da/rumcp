package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/babang1da/rumcp/server/internal/api"
	"github.com/babang1da/rumcp/server/internal/auth"
	"github.com/babang1da/rumcp/server/internal/store"
	"github.com/babang1da/rumcp/server/internal/store/storetest"
)

type env struct {
	repo    *storetest.Memory
	session *auth.Session
	handler *api.Handler
	user    store.User
	cookie  *http.Cookie
}

func newEnv(t *testing.T) *env {
	t.Helper()
	repo := storetest.New()
	user, err := repo.UpsertUser(t.Context(), store.User{
		YandexID: "100500", Login: "egor", Email: "egor@example.com", Name: "Egor",
	})
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	session := auth.NewSession([]byte("session-secret-long-enough"), false)

	recorder := httptest.NewRecorder()
	session.Set(recorder, user.ID)
	cookie := recorder.Result().Cookies()[0]

	return &env{
		repo:    repo,
		session: session,
		user:    user,
		cookie:  cookie,
		handler: &api.Handler{Repo: repo, Session: session, DailyLimit: 100},
	}
}

func (e *env) request(method, path, body string, signedIn bool) *httptest.ResponseRecorder {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	if signedIn {
		req.AddCookie(e.cookie)
	}
	recorder := httptest.NewRecorder()
	switch {
	case path == "/api/me":
		e.handler.Me(recorder, req)
	case path == "/api/disconnect":
		e.handler.Disconnect(recorder, req)
	case path == "/api/usage":
		e.handler.Usage(recorder, req)
	case path == "/api/leads":
		e.handler.Leads(recorder, req)
	default:
		panic("unknown path " + path)
	}
	return recorder
}

func decode(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	return payload
}

func TestMeRequiresSession(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{"/api/me", "/api/usage"} {
		recorder := e.request(http.MethodGet, path, "", false)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s status %d, want 401", path, recorder.Code)
		}
	}
	if recorder := e.request(http.MethodPost, "/api/disconnect", "", false); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("disconnect status %d, want 401", recorder.Code)
	}
}

func TestMeReturnsUserAndUsage(t *testing.T) {
	e := newEnv(t)
	connected := time.Now().Add(time.Hour)
	if err := e.repo.SaveAccount(t.Context(), store.Account{
		UserID:         e.user.ID,
		Provider:       "yandex",
		AccessTokenEnc: "v1:sealed",
		Scope:          "metrika:read",
		ExpiresAt:      &connected,
	}); err != nil {
		t.Fatalf("SaveAccount: %v", err)
	}
	if err := e.repo.RecordCall(t.Context(), e.user.ID, "metrika", "get_counters", "ok"); err != nil {
		t.Fatalf("RecordCall: %v", err)
	}

	recorder := e.request(http.MethodGet, "/api/me", "", true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	payload := decode(t, recorder)

	user, _ := payload["user"].(map[string]any)
	if user["login"] != "egor" || user["email"] != "egor@example.com" {
		t.Fatalf("user = %v", user)
	}
	account, _ := payload["account"].(map[string]any)
	if account["connected"] != true {
		t.Fatalf("account = %v, want connected", account)
	}
	usage, _ := payload["usage"].(map[string]any)
	if usage["calls_today"] != float64(1) || usage["limit"] != float64(100) {
		t.Fatalf("usage = %v", usage)
	}
}

func TestMeWithoutAccount(t *testing.T) {
	e := newEnv(t)
	payload := decode(t, e.request(http.MethodGet, "/api/me", "", true))
	account, _ := payload["account"].(map[string]any)
	if account["connected"] != false {
		t.Fatalf("account = %v, want not connected", account)
	}
}

func TestUsageCountsCallsPerConnector(t *testing.T) {
	e := newEnv(t)
	for _, connector := range []string{"metrika", "metrika", "webmaster"} {
		if err := e.repo.RecordCall(t.Context(), e.user.ID, connector, "tool", "ok"); err != nil {
			t.Fatalf("RecordCall: %v", err)
		}
	}

	recorder := e.request(http.MethodGet, "/api/usage", "", true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", recorder.Code)
	}
	payload := decode(t, recorder)
	if payload["calls_today"] != float64(3) {
		t.Fatalf("calls_today = %v, want 3", payload["calls_today"])
	}
	if payload["remaining"] != float64(97) {
		t.Fatalf("remaining = %v, want 97", payload["remaining"])
	}
	byConnector, _ := payload["by_connector"].(map[string]any)
	if byConnector["metrika"] != float64(2) || byConnector["webmaster"] != float64(1) {
		t.Fatalf("by_connector = %v", byConnector)
	}
}

func TestDisconnectRemovesAccount(t *testing.T) {
	e := newEnv(t)
	if err := e.repo.SaveAccount(t.Context(), store.Account{
		UserID: e.user.ID, Provider: "yandex", AccessTokenEnc: "v1:sealed",
	}); err != nil {
		t.Fatalf("SaveAccount: %v", err)
	}

	recorder := e.request(http.MethodPost, "/api/disconnect", "", true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", recorder.Code)
	}
	if _, err := e.repo.AccountByUser(context.Background(), e.user.ID, "yandex"); err == nil {
		t.Fatal("the account was not deleted")
	}
	payload := decode(t, e.request(http.MethodGet, "/api/me", "", true))
	account, _ := payload["account"].(map[string]any)
	if account["connected"] != false {
		t.Fatalf("account = %v after disconnect, want not connected", account)
	}
}

func TestLeadsStoresRequest(t *testing.T) {
	e := newEnv(t)
	recorder := e.request(http.MethodPost, "/api/leads", `{"email":" Buyer@Example.COM ","service":"direct"}`, false)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status %d, want 201: %s", recorder.Code, recorder.Body.String())
	}

	leads := e.repo.Leads()
	if len(leads) != 1 {
		t.Fatalf("%d leads stored, want 1", len(leads))
	}
	if leads[0].Email != "buyer@example.com" {
		t.Errorf("email = %q, want trimmed and lowercased", leads[0].Email)
	}
	if leads[0].Service != "direct" {
		t.Errorf("service = %q", leads[0].Service)
	}
}

func TestLeadsAttachesSignedInUser(t *testing.T) {
	e := newEnv(t)
	if recorder := e.request(http.MethodPost, "/api/leads", `{"email":"buyer@example.com","service":"wordstat"}`, true); recorder.Code != http.StatusCreated {
		t.Fatalf("status %d, want 201", recorder.Code)
	}
	leads := e.repo.Leads()
	if len(leads) != 1 || leads[0].UserID != e.user.ID {
		t.Fatalf("leads = %+v, want the session user attached", leads)
	}
}

func TestLeadsRejectsBadPayload(t *testing.T) {
	e := newEnv(t)
	for _, body := range []string{`{"email":"not-an-email","service":"direct"}`, `{"email":"a@b.ru"}`, `{}`, `not-json`} {
		if recorder := e.request(http.MethodPost, "/api/leads", body, false); recorder.Code != http.StatusBadRequest {
			t.Errorf("body %s: status %d, want 400", body, recorder.Code)
		}
	}
}

func TestPostOnlyEndpoints(t *testing.T) {
	e := newEnv(t)
	if recorder := e.request(http.MethodGet, "/api/disconnect", "", true); recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("disconnect status %d, want 405", recorder.Code)
	}
	if recorder := e.request(http.MethodGet, "/api/leads", "", true); recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("leads status %d, want 405", recorder.Code)
	}
}
