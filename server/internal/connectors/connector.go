// Package connectors описывает контракт коннектора к внешнему сервису.
// Один коннектор — один каталог internal/connectors/<имя>/ со списком
// инструментов только на чтение.
package connectors

import (
	"context"
	"encoding/json"
	"net/http"
)

// Env — то, что мост MCP передаёт инструменту: токен Яндекса пользователя,
// чей запрос обслуживается. Коннектор не ходит в БД и не знает про OAuth сервиса.
type Env struct {
	// AccessToken — OAuth-токен подключённого аккаунта Яндекса.
	AccessToken string
	// Client — HTTP-клиент; nil означает клиент коннектора по умолчанию.
	Client *http.Client
}

// Tool — инструмент, который коннектор отдаёт MCP-клиенту.
type Tool struct {
	// Name — имя инструмента в MCP, уникальное в пределах коннектора.
	// В MCP инструмент называется "<коннектор>_<имя>".
	Name string
	// Description — описание для ассистента.
	Description string
	// InputSchema — JSON Schema (draft 2020-12, type "object") аргументов.
	InputSchema map[string]any
	// Call выполняет инструмент. Аргументы — сырые аргументы MCP, инструмент
	// сам проверяет их по InputSchema.
	Call func(ctx context.Context, env Env, args json.RawMessage) (any, error)
}

// Connector — внешний сервис, доступный через /mcp.
type Connector interface {
	// Name — имя коннектора, попадает в журнал вызовов и в имена инструментов.
	Name() string
	// Tools — инструменты коннектора.
	Tools() []Tool
}
