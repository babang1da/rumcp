// Package metrika exposes the read-only Yandex Metrica API as MCP tools:
// counters, table and bytime reports, goals and traffic sources.
//
// The tool set follows the open source servers of the same service (see
// LICENSES.md); the code here is written from the public Metrica API docs.
package metrika

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/babang1da/rumcp/server/internal/connectors"
)

// Name is the connector name: it prefixes MCP tool names and goes to call_log.
const Name = "metrika"

// DefaultBaseURL is the public Metrica API endpoint.
const DefaultBaseURL = "https://api-metrika.yandex.net"

// maxBody caps a response body read from Metrica, 8 MiB is far above a full
// report page and keeps one bad call from eating the service memory.
const maxBody = 8 << 20

// Connector implements connectors.Connector for Yandex Metrica.
type Connector struct {
	// BaseURL is the API endpoint; empty means DefaultBaseURL. Tests point it
	// at a stub server.
	BaseURL string
	// Client is used when connectors.Env has no client.
	Client *http.Client
}

// New returns the Metrica connector.
func New() *Connector {
	return &Connector{
		BaseURL: DefaultBaseURL,
		Client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Name implements connectors.Connector.
func (c *Connector) Name() string { return Name }

// Tools implements connectors.Connector.
func (c *Connector) Tools() []connectors.Tool {
	return []connectors.Tool{
		{
			Name:        "counters",
			Description: "Список счётчиков Яндекс Метрики, к которым у пользователя есть доступ: id, название, сайт, статус. Нужен первым, чтобы узнать counter_id для остальных инструментов.",
			InputSchema: objectSchema(map[string]any{}),
			Call:        c.counters,
		},
		{
			Name:        "table",
			Description: "Отчёт Яндекс Метрики в виде таблицы: строки — комбинации измерений, в конце итоги. Например, визиты и посетители по источникам трафика или страницам за период.",
			InputSchema: objectSchema(map[string]any{
				"counter_id": intProp("Номер счётчика Метрики."),
				"metrics":    arrayProp("Метрики отчёта, например ym:s:visits, ym:s:users, ym:s:bounceRate.", []string{"ym:s:visits", "ym:s:users"}),
				"dimensions": arrayProp("Измерения отчёта, например ym:s:lastsignTrafficSource, ym:s:startURL. Пусто — только итоги.", []string{}),
				"date1":      stringProp("Начало периода: YYYY-MM-DD или 7daysAgo, yesterday, today.", "7daysAgo"),
				"date2":      stringProp("Конец периода: YYYY-MM-DD или today, yesterday.", "today"),
				"filters":    stringProp("Фильтр Метрики, например ym:s:deviceCategory=='mobile'.", ""),
				"sort":       stringProp("Сортировка, например -ym:s:visits (по убыванию).", ""),
				"limit":      limitProp("Сколько строк вернуть, 1–1000.", 100),
				"accuracy":   enumProp("Точность отчёта: low (быстро), medium, full (точно).", []string{"low", "medium", "full"}, "full"),
			}, "counter_id"),
			Call: c.table,
		},
		{
			Name:        "bytime",
			Description: "Отчёт Яндекс Метрики по времени: метрики по дням, неделям или месяцам за период. Для графиков и динамики.",
			InputSchema: objectSchema(map[string]any{
				"counter_id": intProp("Номер счётчика Метрики."),
				"metrics":    arrayProp("Метрики отчёта, например ym:s:visits, ym:s:users.", []string{"ym:s:visits", "ym:s:users"}),
				"dimensions": arrayProp("Дополнительные измерения, например ym:s:lastsignTrafficSource.", []string{}),
				"date1":      stringProp("Начало периода: YYYY-MM-DD или 7daysAgo, yesterday, today.", "7daysAgo"),
				"date2":      stringProp("Конец периода: YYYY-MM-DD или today, yesterday.", "today"),
				"group":      enumProp("Шаг времени: day, week или month.", []string{"day", "week", "month"}, "day"),
				"filters":    stringProp("Фильтр Метрики, например ym:s:deviceCategory=='mobile'.", ""),
				"accuracy":   enumProp("Точность отчёта: low (быстро), medium, full (точно).", []string{"low", "medium", "full"}, "full"),
			}, "counter_id"),
			Call: c.bytime,
		},
		{
			Name:        "goals",
			Description: "Цели счётчика Яндекс Метрики: id, название, тип. Нужны, чтобы считать конверсии в отчётах.",
			InputSchema: objectSchema(map[string]any{
				"counter_id": intProp("Номер счётчика Метрики."),
			}, "counter_id"),
			Call: c.goals,
		},
		{
			Name:        "traffic_sources",
			Description: "Источники трафика счётчика Яндекс Метрики за период: визиты, посетители и отказы по каналам (organic, ad, direct, referral, social).",
			InputSchema: objectSchema(map[string]any{
				"counter_id":  intProp("Номер счётчика Метрики."),
				"date1":       stringProp("Начало периода: YYYY-MM-DD или 7daysAgo, yesterday, today.", "7daysAgo"),
				"date2":       stringProp("Конец периода: YYYY-MM-DD или today, yesterday.", "today"),
				"attribution": enumProp("Атрибуция источника: lastsign (по последнему визиту), last, first.", []string{"lastsign", "last", "first"}, "lastsign"),
				"limit":       limitProp("Сколько строк вернуть, 1–1000.", 100),
			}, "counter_id"),
			Call: c.trafficSources,
		},
	}
}

// counters lists the counters of the user.
func (c *Connector) counters(ctx context.Context, env connectors.Env, _ json.RawMessage) (any, error) {
	var payload struct {
		Counters []struct {
			ID     int64  `json:"id"`
			Name   string `json:"name"`
			Site   string `json:"site"`
			Status string `json:"status"`
		} `json:"counters"`
	}
	if err := c.get(ctx, env, "/management/v1/counters", nil, &payload); err != nil {
		return nil, err
	}
	type counter struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Site   string `json:"site"`
		Status string `json:"status"`
	}
	counters := make([]counter, 0, len(payload.Counters))
	for _, item := range payload.Counters {
		counters = append(counters, counter{ID: item.ID, Name: item.Name, Site: item.Site, Status: item.Status})
	}
	return map[string]any{"counters": counters}, nil
}

type tableArgs struct {
	CounterID  int64    `json:"counter_id"`
	Metrics    []string `json:"metrics"`
	Dimensions []string `json:"dimensions"`
	Date1      string   `json:"date1"`
	Date2      string   `json:"date2"`
	Filters    string   `json:"filters"`
	Sort       string   `json:"sort"`
	Limit      int      `json:"limit"`
	Accuracy   string   `json:"accuracy"`
}

// table returns a /stat/v1/data report.
func (c *Connector) table(ctx context.Context, env connectors.Env, args json.RawMessage) (any, error) {
	var in tableArgs
	if err := decode(args, &in); err != nil {
		return nil, err
	}
	if err := checkCounter(in.CounterID); err != nil {
		return nil, err
	}
	if len(in.Metrics) == 0 {
		return nil, fmt.Errorf("Метрика: укажите хотя бы одну метрику в metrics")
	}
	query := reportQuery(in.CounterID, in.Metrics, in.Dimensions, in.Date1, in.Date2, in.Filters, in.Accuracy)
	query.Set("sort", in.Sort)
	query.Set("limit", strconv.Itoa(clampLimit(in.Limit)))
	return c.report(ctx, env, "/stat/v1/data", query)
}

type bytimeArgs struct {
	tableArgs
	Group string `json:"group"`
}

// bytime returns a /stat/v1/data/bytime report.
func (c *Connector) bytime(ctx context.Context, env connectors.Env, args json.RawMessage) (any, error) {
	var in bytimeArgs
	if err := decode(args, &in); err != nil {
		return nil, err
	}
	if err := checkCounter(in.CounterID); err != nil {
		return nil, err
	}
	if len(in.Metrics) == 0 {
		return nil, fmt.Errorf("Метрика: укажите хотя бы одну метрику в metrics")
	}
	query := reportQuery(in.CounterID, in.Metrics, in.Dimensions, in.Date1, in.Date2, in.Filters, in.Accuracy)
	group := in.Group
	if group == "" {
		group = "day"
	}
	query.Set("group", group)
	return c.report(ctx, env, "/stat/v1/data/bytime", query)
}

// goals lists the goals of one counter.
func (c *Connector) goals(ctx context.Context, env connectors.Env, args json.RawMessage) (any, error) {
	var in struct {
		CounterID int64 `json:"counter_id"`
	}
	if err := decode(args, &in); err != nil {
		return nil, err
	}
	if err := checkCounter(in.CounterID); err != nil {
		return nil, err
	}
	var payload struct {
		Goals []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"goals"`
	}
	path := "/management/v1/counter/" + strconv.FormatInt(in.CounterID, 10) + "/goals"
	if err := c.get(ctx, env, path, nil, &payload); err != nil {
		return nil, err
	}
	type goal struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	}
	goals := make([]goal, 0, len(payload.Goals))
	for _, item := range payload.Goals {
		goals = append(goals, goal{ID: item.ID, Name: item.Name, Type: item.Type})
	}
	return map[string]any{"counter_id": in.CounterID, "goals": goals}, nil
}

// trafficSources returns visits and users by traffic channel.
func (c *Connector) trafficSources(ctx context.Context, env connectors.Env, args json.RawMessage) (any, error) {
	var in struct {
		CounterID   int64  `json:"counter_id"`
		Date1       string `json:"date1"`
		Date2       string `json:"date2"`
		Attribution string `json:"attribution"`
		Limit       int    `json:"limit"`
	}
	if err := decode(args, &in); err != nil {
		return nil, err
	}
	if err := checkCounter(in.CounterID); err != nil {
		return nil, err
	}
	query := reportQuery(
		in.CounterID,
		[]string{"ym:s:visits", "ym:s:users", "ym:s:bounceRate"},
		[]string{"ym:s:lastsignTrafficSource"},
		in.Date1, in.Date2, "", "full",
	)
	query.Set("attribution", defaultString(in.Attribution, "lastsign"))
	query.Set("sort", "-ym:s:visits")
	query.Set("limit", strconv.Itoa(clampLimit(in.Limit)))
	return c.report(ctx, env, "/stat/v1/data", query)
}

// report runs a report query and returns the Metrica JSON as is: the client is
// an assistant, and the raw payload keeps dimensions, totals and intervals.
func (c *Connector) report(ctx context.Context, env connectors.Env, path string, query url.Values) (any, error) {
	var payload json.RawMessage
	if err := c.get(ctx, env, path, query, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// reportQuery builds the shared part of a report query.
func reportQuery(counterID int64, metrics, dimensions []string, date1, date2, filters, accuracy string) url.Values {
	query := url.Values{
		"ids":     {strconv.FormatInt(counterID, 10)},
		"metrics": {strings.Join(metrics, ",")},
		"date1":   {defaultString(date1, "7daysAgo")},
		"date2":   {defaultString(date2, "today")},
	}
	if len(dimensions) > 0 {
		query.Set("dimensions", strings.Join(dimensions, ","))
	}
	if filters != "" {
		query.Set("filters", filters)
	}
	if accuracy != "" {
		query.Set("accuracy", accuracy)
	}
	return query
}

// get calls the Metrica API and decodes the response into out.
func (c *Connector) get(ctx context.Context, env connectors.Env, path string, query url.Values, out any) error {
	if env.AccessToken == "" {
		return fmt.Errorf("Метрика: нет токена Яндекса")
	}
	endpoint := strings.TrimSuffix(c.BaseURL, "/")
	if endpoint == "" {
		endpoint = DefaultBaseURL
	}
	endpoint += path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("Метрика: запрос: %w", err)
	}
	req.Header.Set("Authorization", "OAuth "+env.AccessToken)
	req.Header.Set("Accept", "application/json")

	client := env.Client
	if client == nil {
		client = c.Client
	}
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Метрика: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("Метрика: ответ: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Метрика: %s: %s", resp.Status, apiError(body))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("Метрика: разбор ответа: %w", err)
	}
	return nil
}

// apiError pulls a human-readable message out of a Metrica error body.
func apiError(body []byte) string {
	var payload struct {
		Errors []struct {
			ErrorType string `json:"error_type"`
			Message   string `json:"message"`
		} `json:"errors"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		if len(payload.Errors) > 0 && payload.Errors[0].Message != "" {
			return payload.Errors[0].Message
		}
		if payload.Message != "" {
			return payload.Message
		}
	}
	text := strings.TrimSpace(string(body))
	if runes := []rune(text); len(runes) > 200 {
		text = string(runes[:200]) + "…"
	}
	return text
}

func decode(args json.RawMessage, dst any) error {
	if len(args) == 0 {
		return nil
	}
	if err := json.Unmarshal(args, dst); err != nil {
		return fmt.Errorf("Метрика: неверные аргументы: %w", err)
	}
	return nil
}

func checkCounter(id int64) error {
	if id <= 0 {
		return fmt.Errorf("Метрика: укажите номер счётчика в counter_id")
	}
	return nil
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func intProp(description string) map[string]any {
	return map[string]any{"type": "integer", "minimum": 1, "description": description}
}

func limitProp(description string, def int) map[string]any {
	return map[string]any{"type": "integer", "minimum": 1, "maximum": 1000, "default": def, "description": description}
}

func stringProp(description, def string) map[string]any {
	prop := map[string]any{"type": "string", "description": description}
	if def != "" {
		prop["default"] = def
	}
	return prop
}

func arrayProp(description string, def []string) map[string]any {
	return map[string]any{
		"type":        "array",
		"items":       map[string]any{"type": "string"},
		"default":     def,
		"description": description,
	}
}

func enumProp(description string, values []string, def string) map[string]any {
	return map[string]any{
		"type":        "string",
		"enum":        values,
		"default":     def,
		"description": description,
	}
}
