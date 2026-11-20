// Package webmaster exposes the read-only Yandex Webmaster API as MCP tools:
// sites, indexing status, popular search queries, site diagnostics and
// sitemaps.
//
// The tool set follows the open source servers of the same service (see
// LICENSES.md); the code here is written from the public Webmaster API docs.
package webmaster

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
const Name = "webmaster"

// DefaultBaseURL is the public Webmaster API v4 endpoint.
const DefaultBaseURL = "https://api.webmaster.yandex.net/v4"

// maxBody caps a response body read from Webmaster, 8 MiB is far above any
// single page of sites, queries or sitemaps and keeps one bad call from eating
// the service memory.
const maxBody = 8 << 20

// Connector implements connectors.Connector for Yandex Webmaster.
type Connector struct {
	// BaseURL is the API endpoint; empty means DefaultBaseURL. Tests point it
	// at a stub server.
	BaseURL string
	// Client is used when connectors.Env has no client.
	Client *http.Client
}

// New returns the Webmaster connector.
func New() *Connector {
	return &Connector{
		BaseURL: DefaultBaseURL,
		Client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Name implements connectors.Connector.
func (c *Connector) Name() string { return Name }

// Tools implements connectors.Connector. Every tool reads: nothing here adds,
// changes or removes anything in the Webmaster account.
func (c *Connector) Tools() []connectors.Tool {
	return []connectors.Tool{
		{
			Name:        "sites",
			Description: "Список сайтов пользователя в Яндекс Вебмастере: host_id, адрес, название, подтверждено ли владение и статус данных. Нужен первым: host_id — аргумент остальных инструментов Вебмастера.",
			InputSchema: objectSchema(map[string]any{}),
			Call:        c.sites,
		},
		{
			Name:        "indexing",
			Description: "Статус индексации сайта в Яндексе: индекс качества (ИКС), сколько страниц в поиске и сколько исключено, сколько у сайта критичных и некритичных проблем.",
			InputSchema: objectSchema(map[string]any{
				"host_id": stringProp("Идентификатор сайта из инструмента sites, например https:example.com:443."),
			}, "host_id"),
			Call: c.indexing,
		},
		{
			Name:        "search_queries",
			Description: "Популярные поисковые запросы сайта в Яндексе: по каждому запросу показы, клики и средние позиции. Показывает, по каким запросам сайт находят в поиске.",
			InputSchema: objectSchema(map[string]any{
				"host_id":         stringProp("Идентификатор сайта из инструмента sites, например https:example.com:443."),
				"order_by":        enumProp("По какому показателю сортировать запросы.", []string{"TOTAL_SHOWS", "TOTAL_CLICKS"}, "TOTAL_SHOWS"),
				"query_indicator": enumProp("Какой показатель вернуть по каждому запросу.", []string{"TOTAL_SHOWS", "TOTAL_CLICKS", "AVG_SHOW_POSITION", "AVG_CLICK_POSITION"}, "TOTAL_SHOWS"),
				"limit":           limitProp("Сколько запросов вернуть, 1–500.", 100, 500),
				"offset":          offsetProp("Сколько запросов пропустить, чтобы получить следующую страницу."),
			}, "host_id"),
			Call: c.searchQueries,
		},
		{
			Name:        "diagnostics",
			Description: "Диагностика сайта в Яндексе: проблемы с критичностью (FATAL, CRITICAL, POSSIBLE_PROBLEM, RECOMMENDATION) и рекомендации, что исправить.",
			InputSchema: objectSchema(map[string]any{
				"host_id": stringProp("Идентификатор сайта из инструмента sites, например https:example.com:443."),
			}, "host_id"),
			Call: c.diagnostics,
		},
		{
			Name:        "sitemaps",
			Description: "Карты сайта (sitemap), которые Яндекс нашёл или принял у сайта: адрес, тип, дата обработки, число URL и ошибки обработки.",
			InputSchema: objectSchema(map[string]any{
				"host_id": stringProp("Идентификатор сайта из инструмента sites, например https:example.com:443."),
				"limit":   limitProp("Сколько карт сайта вернуть, 1–500.", 50, 500),
				"from":    stringProp("Идентификатор карты сайта, с которой продолжить список, для следующей страницы."),
			}, "host_id"),
			Call: c.sitemaps,
		},
	}
}

// sites lists the sites of the user.
func (c *Connector) sites(ctx context.Context, env connectors.Env, _ json.RawMessage) (any, error) {
	userID, err := c.userID(ctx, env)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Hosts []struct {
			HostID          string `json:"host_id"`
			ASCIIHostURL    string `json:"ascii_host_url"`
			UnicodeHostURL  string `json:"unicode_host_url"`
			Verified        bool   `json:"verified"`
			HostDataStatus  string `json:"host_data_status"`
			HostDisplayName string `json:"host_display_name"`
		} `json:"hosts"`
	}
	if err := c.get(ctx, env, hostsPath(userID), nil, &payload); err != nil {
		return nil, err
	}
	type site struct {
		HostID   string `json:"host_id"`
		URL      string `json:"url"`
		Name     string `json:"name"`
		Verified bool   `json:"verified"`
		Status   string `json:"status"`
	}
	sites := make([]site, 0, len(payload.Hosts))
	for _, item := range payload.Hosts {
		siteURL := item.UnicodeHostURL
		if siteURL == "" {
			siteURL = item.ASCIIHostURL
		}
		sites = append(sites, site{
			HostID:   item.HostID,
			URL:      siteURL,
			Name:     item.HostDisplayName,
			Verified: item.Verified,
			Status:   item.HostDataStatus,
		})
	}
	return map[string]any{"sites": sites}, nil
}

// indexing returns the summary of the site: SQI, pages in search and excluded,
// problems by severity.
func (c *Connector) indexing(ctx context.Context, env connectors.Env, args json.RawMessage) (any, error) {
	var in struct {
		HostID string `json:"host_id"`
	}
	if err := decode(args, &in); err != nil {
		return nil, err
	}
	path, err := c.hostPath(ctx, env, in.HostID)
	if err != nil {
		return nil, err
	}
	return c.raw(ctx, env, path+"/summary", nil)
}

type searchQueriesArgs struct {
	HostID         string `json:"host_id"`
	OrderBy        string `json:"order_by"`
	QueryIndicator string `json:"query_indicator"`
	Limit          int    `json:"limit"`
	Offset         int    `json:"offset"`
}

// searchQueries returns the popular search queries of the site.
func (c *Connector) searchQueries(ctx context.Context, env connectors.Env, args json.RawMessage) (any, error) {
	var in searchQueriesArgs
	if err := decode(args, &in); err != nil {
		return nil, err
	}
	path, err := c.hostPath(ctx, env, in.HostID)
	if err != nil {
		return nil, err
	}
	query := url.Values{
		"order_by":        {defaultString(in.OrderBy, "TOTAL_SHOWS")},
		"query_indicator": {defaultString(in.QueryIndicator, "TOTAL_SHOWS")},
		"limit":           {strconv.Itoa(clampLimit(in.Limit, 100, 500))},
		"offset":          {strconv.Itoa(clampOffset(in.Offset))},
	}
	return c.raw(ctx, env, path+"/search-queries/popular", query)
}

// diagnostics returns the problems Yandex found on the site.
func (c *Connector) diagnostics(ctx context.Context, env connectors.Env, args json.RawMessage) (any, error) {
	var in struct {
		HostID string `json:"host_id"`
	}
	if err := decode(args, &in); err != nil {
		return nil, err
	}
	path, err := c.hostPath(ctx, env, in.HostID)
	if err != nil {
		return nil, err
	}
	return c.raw(ctx, env, path+"/diagnostics", nil)
}

// sitemaps returns the sitemaps of the site.
func (c *Connector) sitemaps(ctx context.Context, env connectors.Env, args json.RawMessage) (any, error) {
	var in struct {
		HostID string `json:"host_id"`
		Limit  int    `json:"limit"`
		From   string `json:"from"`
	}
	if err := decode(args, &in); err != nil {
		return nil, err
	}
	path, err := c.hostPath(ctx, env, in.HostID)
	if err != nil {
		return nil, err
	}
	query := url.Values{"limit": {strconv.Itoa(clampLimit(in.Limit, 50, 500))}}
	if in.From != "" {
		query.Set("from", in.From)
	}
	return c.raw(ctx, env, path+"/sitemaps", query)
}

// raw runs a request and returns the Webmaster JSON as is: the client is an
// assistant, and the raw payload keeps every field of the answer.
func (c *Connector) raw(ctx context.Context, env connectors.Env, path string, query url.Values) (any, error) {
	var payload json.RawMessage
	if err := c.get(ctx, env, path, query, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// userID resolves the Webmaster user of the token: the endpoints of a site
// live under /user/{user_id}.
func (c *Connector) userID(ctx context.Context, env connectors.Env) (int64, error) {
	var payload struct {
		UserID int64 `json:"user_id"`
	}
	if err := c.get(ctx, env, "/user", nil, &payload); err != nil {
		return 0, err
	}
	if payload.UserID <= 0 {
		return 0, fmt.Errorf("Вебмастер: Яндекс не вернул user_id")
	}
	return payload.UserID, nil
}

// hostPath returns the base path of one site.
func (c *Connector) hostPath(ctx context.Context, env connectors.Env, hostID string) (string, error) {
	if hostID == "" {
		return "", fmt.Errorf("Вебмастер: укажите идентификатор сайта в host_id, его отдаёт инструмент sites")
	}
	userID, err := c.userID(ctx, env)
	if err != nil {
		return "", err
	}
	return "/user/" + strconv.FormatInt(userID, 10) + "/hosts/" + hostSegment(hostID), nil
}

// hostSegment escapes a site id for the path. Webmaster documents the id
// percent-encoded (https%3Aexample.com%3A443) and url.PathEscape leaves the
// colon of "https:example.com:443" alone, so it is escaped here.
func hostSegment(hostID string) string {
	return strings.ReplaceAll(url.PathEscape(hostID), ":", "%3A")
}

// hostsPath is the list of sites of the user.
func hostsPath(userID int64) string {
	return "/user/" + strconv.FormatInt(userID, 10) + "/hosts"
}

// get calls the Webmaster API and decodes the response into out.
func (c *Connector) get(ctx context.Context, env connectors.Env, path string, query url.Values, out any) error {
	if env.AccessToken == "" {
		return fmt.Errorf("Вебмастер: нет токена Яндекса")
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
		return fmt.Errorf("Вебмастер: запрос: %w", err)
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
		return fmt.Errorf("Вебмастер: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("Вебмастер: ответ: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Вебмастер: %s: %s", resp.Status, apiError(body))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("Вебмастер: разбор ответа: %w", err)
	}
	return nil
}

// apiError pulls a human-readable message out of a Webmaster error body.
func apiError(body []byte) string {
	var payload struct {
		ErrorCode    string `json:"error_code"`
		ErrorMessage string `json:"error_message"`
		Errors       []struct {
			ErrorCode    string `json:"error_code"`
			ErrorMessage string `json:"error_message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		if payload.ErrorMessage != "" {
			return payload.ErrorMessage
		}
		if len(payload.Errors) > 0 && payload.Errors[0].ErrorMessage != "" {
			return payload.Errors[0].ErrorMessage
		}
		if payload.ErrorCode != "" {
			return payload.ErrorCode
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
		return fmt.Errorf("Вебмастер: неверные аргументы: %w", err)
	}
	return nil
}

func clampLimit(limit, def, max int) int {
	if limit <= 0 {
		return def
	}
	if limit > max {
		return max
	}
	return limit
}

func clampOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
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

func stringProp(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func limitProp(description string, def, max int) map[string]any {
	return map[string]any{
		"type":        "integer",
		"minimum":     1,
		"maximum":     max,
		"default":     def,
		"description": description,
	}
}

func offsetProp(description string) map[string]any {
	return map[string]any{
		"type":        "integer",
		"minimum":     0,
		"default":     0,
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
