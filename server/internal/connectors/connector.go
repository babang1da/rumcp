// Package connectors описывает контракт коннектора к внешнему сервису.
// Один коннектор — один каталог internal/connectors/<имя>/ со списком
// инструментов только на чтение.
package connectors

// Tool — инструмент, который коннектор отдаёт MCP-клиенту.
type Tool struct {
	// Name — имя инструмента в MCP, уникальное в пределах коннектора.
	Name string
	// Description — описание для ассистента.
	Description string
}

// Connector — внешний сервис, доступный через /mcp.
type Connector interface {
	// Name — имя коннектора, попадает в журнал вызовов и в имена инструментов.
	Name() string
	// Tools — инструменты коннектора.
	Tools() []Tool
}
