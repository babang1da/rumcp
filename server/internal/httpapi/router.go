// Package httpapi собирает HTTP-маршруты сервиса.
package httpapi

import "net/http"

// NewRouter возвращает маршруты сервиса. Кроме /healthz пока ничего нет:
// /mcp, /oauth/*, /.well-known/* и /api/* появляются в следующих карточках эпика.
func NewRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	return mux
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok","service":"server"}`))
}
