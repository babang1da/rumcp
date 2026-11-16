// Package api serves the cabinet endpoints under /api.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/babang1da/rumcp/server/internal/store"
)

// Repository is the data access the cabinet needs.
type Repository interface {
	UserByID(ctx context.Context, id string) (store.User, error)
	AccountByUser(ctx context.Context, userID, provider string) (store.Account, error)
	DeleteAccount(ctx context.Context, userID, provider string) error
	Usage(ctx context.Context, userID string, since time.Time) (store.Usage, error)
	CreateLead(ctx context.Context, l store.Lead) error
}

// SessionReader tells which user is signed in.
type SessionReader interface {
	UserID(r *http.Request) (string, bool)
}

// Handler serves the cabinet API.
type Handler struct {
	Repo       Repository
	Session    SessionReader
	DailyLimit int
	Now        func() time.Time
}

// Me returns the signed-in user, the connected account and today's usage.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	user, ok := h.user(w, r)
	if !ok {
		return
	}

	response := map[string]any{
		"user":    userView(user),
		"account": map[string]any{"connected": false},
	}
	if account, err := h.Repo.AccountByUser(r.Context(), user.ID, "yandex"); err == nil {
		view := map[string]any{"connected": true, "connected_at": account.ConnectedAt}
		if account.ExpiresAt != nil {
			view["expires_at"] = *account.ExpiresAt
		}
		view["scope"] = account.Scope
		response["account"] = view
	}

	usage, err := h.Repo.Usage(r.Context(), user.ID, h.dayStart())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "usage_failed")
		return
	}
	response["usage"] = map[string]any{"calls_today": usage.CallsToday, "limit": h.DailyLimit}

	writeJSON(w, http.StatusOK, response)
}

// Disconnect removes the Yandex account of the user.
func (h *Handler) Disconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	user, ok := h.user(w, r)
	if !ok {
		return
	}
	if err := h.Repo.DeleteAccount(r.Context(), user.ID, "yandex"); err != nil {
		writeError(w, http.StatusInternalServerError, "disconnect_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connected": false})
}

// Usage returns today's call counts of the user.
func (h *Handler) Usage(w http.ResponseWriter, r *http.Request) {
	user, ok := h.user(w, r)
	if !ok {
		return
	}
	usage, err := h.Repo.Usage(r.Context(), user.ID, h.dayStart())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "usage_failed")
		return
	}
	remaining := h.DailyLimit - usage.CallsToday
	if remaining < 0 {
		remaining = 0
	}
	byConnector := usage.ByConnector
	if byConnector == nil {
		byConnector = map[string]int{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"calls_today":  usage.CallsToday,
		"limit":        h.DailyLimit,
		"remaining":    remaining,
		"by_connector": byConnector,
	})
}

// Leads stores a request for a connector that is not released yet.
func (h *Handler) Leads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}

	var payload struct {
		Email   string `json:"email"`
		Service string `json:"service"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}

	email := strings.TrimSpace(strings.ToLower(payload.Email))
	if !validEmail(email) {
		writeError(w, http.StatusBadRequest, "invalid_email")
		return
	}
	service := strings.TrimSpace(strings.ToLower(payload.Service))
	if service == "" || len(service) > 64 {
		writeError(w, http.StatusBadRequest, "invalid_service")
		return
	}

	lead := store.Lead{Email: email, Service: service}
	if userID, ok := h.Session.UserID(r); ok {
		lead.UserID = userID
	}
	if err := h.Repo.CreateLead(r.Context(), lead); err != nil {
		writeError(w, http.StatusInternalServerError, "lead_failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

// user resolves the session user or writes 401.
func (h *Handler) user(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	userID, ok := h.Session.UserID(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return store.User{}, false
	}
	user, err := h.Repo.UserByID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return store.User{}, false
		}
		writeError(w, http.StatusInternalServerError, "user_failed")
		return store.User{}, false
	}
	return user, true
}

func (h *Handler) dayStart() time.Time {
	now := time.Now()
	if h.Now != nil {
		now = h.Now()
	}
	now = now.UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

func userView(user store.User) map[string]any {
	return map[string]any{
		"id":    user.ID,
		"login": user.Login,
		"email": user.Email,
		"name":  user.Name,
	}
}

func validEmail(email string) bool {
	if len(email) < 5 || len(email) > 254 {
		return false
	}
	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 {
		return false
	}
	return strings.Contains(email[at+1:], ".") && !strings.ContainsAny(email, " \t\n")
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}
