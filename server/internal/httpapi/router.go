// Package httpapi собирает HTTP-маршруты сервиса.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/babang1da/rumcp/server/internal/api"
	"github.com/babang1da/rumcp/server/internal/auth"
	"github.com/babang1da/rumcp/server/internal/config"
	"github.com/babang1da/rumcp/server/internal/connectors"
	"github.com/babang1da/rumcp/server/internal/connectors/metrika"
	"github.com/babang1da/rumcp/server/internal/connectors/webmaster"
	mcpbridge "github.com/babang1da/rumcp/server/internal/mcp"
	"github.com/babang1da/rumcp/server/internal/oauth"
	"github.com/babang1da/rumcp/server/internal/secret"
	"github.com/babang1da/rumcp/server/internal/store"
)

// NewRouter возвращает маршруты сервиса на одном домене: /healthz, /mcp, OAuth 2.1
// для MCP-клиентов, вход через Яндекс и API кабинета.
func NewRouter(cfg config.Config, repo store.Repository) (http.Handler, error) {
	return NewRouterWithYandex(cfg, repo, nil)
}

// NewRouterWithYandex собирает маршруты с явным клиентом Яндекса: nil означает
// настоящие эндпоинты Яндекса, тесты подставляют свой.
func NewRouterWithYandex(cfg config.Config, repo store.Repository, yandex auth.YandexAPI) (http.Handler, error) {
	box, err := secret.NewBox(cfg.TokenKey)
	if err != nil {
		return nil, err
	}
	session := auth.NewSession(cfg.SessionSecret, cfg.Secure())

	if yandex == nil {
		yandex = auth.NewHTTPYandex(cfg.YandexClientID, cfg.YandexClientSecret)
	}

	authHandler := &auth.Handler{
		Repo:    repo,
		Yandex:  yandex,
		Session: session,
		Box:     box,
		Cfg: auth.Config{
			BaseURL:      cfg.BaseURL,
			ClientID:     cfg.YandexClientID,
			ClientSecret: cfg.YandexClientSecret,
			TestToken:    cfg.YandexTestToken,
			DevLogin:     cfg.DevLoginEnabled(),
		},
	}

	oauthCfg := oauth.DefaultConfig(cfg.BaseURL)
	oauthCfg.CSRFKey = cfg.SessionSecret
	oauthHandler := &oauth.Handler{
		Repo:    repo,
		Session: session,
		Cfg:     oauthCfg,
	}

	apiHandler := &api.Handler{
		Repo:       repo,
		Session:    session,
		DailyLimit: cfg.DailyLimit,
	}
	bridge, err := mcpbridge.New(mcpbridge.Deps{
		Repo:       repo,
		Box:        box,
		Connectors: []connectors.Connector{metrika.New(), webmaster.New()},
		DailyLimit: cfg.DailyLimit,
		Yandex:     mcpbridge.NewYandexRefresher(cfg.YandexClientID, cfg.YandexClientSecret),
		BaseURL:    cfg.BaseURL,
	})
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz(repo))

	// Удалённый MCP для Claude, ChatGPT, Cursor и других клиентов. Без Bearer
	// сервиса запрос получает 401 и WWW-Authenticate от OAuth-обработчика.
	mux.Handle("/mcp", bridge.Handler(oauthHandler))

	// OAuth 2.1 для MCP-клиентов.
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", oauthHandler.Metadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", oauthHandler.ProtectedResource)
	mux.HandleFunc("POST /oauth/register", oauthHandler.Register)
	// GET показывает экран согласия, POST применяет решение пользователя.
	mux.HandleFunc("/oauth/authorize", oauthHandler.Authorize)
	mux.HandleFunc("POST /oauth/token", oauthHandler.Token)

	// Вход в кабинет через Яндекс.
	mux.HandleFunc("GET /oauth/yandex/login", authHandler.Login)
	mux.HandleFunc("GET "+auth.CallbackPath, authHandler.Callback)

	// API кабинета.
	mux.HandleFunc("GET /api/me", apiHandler.Me)
	mux.HandleFunc("POST /api/disconnect", apiHandler.Disconnect)
	mux.HandleFunc("GET /api/usage", apiHandler.Usage)
	mux.HandleFunc("POST /api/leads", apiHandler.Leads)

	// Статистика вызовов для замера метрик.
	mux.HandleFunc("GET /api/stats", statsHandler(cfg.StatsToken, repo))

	return mux, nil
}

// healthz отвечает 200, пока сервис может обратиться к своей БД.
func healthz(repo store.Repository) http.HandlerFunc {
	probe, ok := repo.(pinger)
	return func(w http.ResponseWriter, r *http.Request) {
		status := http.StatusOK
		payload := map[string]string{"status": "ok", "service": "server"}
		if ok {
			if err := probe.Ping(r.Context()); err != nil {
				status = http.StatusServiceUnavailable
				payload["status"] = "degraded"
				payload["database"] = "unavailable"
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(payload)
	}
}

// pinger реализует хранилище на Postgres.
type pinger interface {
	Ping(ctx context.Context) error
}
