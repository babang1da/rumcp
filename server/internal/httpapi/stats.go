// Package httpapi собирает HTTP-маршруты сервиса.
package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/babang1da/rumcp/server/internal/store"
)

// statsResponse — тело ответа GET /api/stats.
type statsResponse struct {
	TotalCalls  int64             `json:"total_calls"`
	Users       []store.UserStats `json:"users"`
	GeneratedAt time.Time         `json:"generated_at"`
}

// statsHandler отдаёт статистику вызовов MCP для замера метрик.
// Доступ разрешён только при совпадении токена с Authorization: Bearer <STATS_TOKEN>.
// Если token пуст, эндпоинт закрыт для всех запросов (401).
func statsHandler(token string, repo store.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !checkStatsToken(r, token) {
			writeErrorJSON(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		stats, err := repo.Stats(r.Context())
		if err != nil {
			writeErrorJSON(w, http.StatusInternalServerError, "stats_failed")
			return
		}

		users := stats.Users
		if users == nil {
			users = []store.UserStats{}
		}

		resp := statsResponse{
			TotalCalls:  stats.TotalCalls,
			Users:       users,
			GeneratedAt: time.Now().UTC(),
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// checkStatsToken сверяет заголовок Authorization: Bearer <token> с ожидаемым значением.
func checkStatsToken(r *http.Request, expected string) bool {
	if expected == "" {
		return false
	}
	auth := r.Header.Get("Authorization")
	token, ok := strings.CutPrefix(auth, "Bearer ")
	if !ok || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(expected)) == 1
}

func writeErrorJSON(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
