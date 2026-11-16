package metrika_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/babang1da/rumcp/server/internal/connectors"
	"github.com/babang1da/rumcp/server/internal/connectors/metrika"
)

// stub is a fake Metrica API: it records the last request and answers with a
// canned body.
type stub struct {
	server   *httptest.Server
	lastPath string
	last     *http.Request
	status   int
	body     string
}

func newStub(t *testing.T, body string) *stub {
	t.Helper()
	s := &stub{status: http.StatusOK, body: body}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.lastPath = r.URL.Path
		s.last = r.Clone(r.Context())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s.status)
		_, _ = w.Write([]byte(s.body))
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *stub) connector() *metrika.Connector {
	c := metrika.New()
	c.BaseURL = s.server.URL
	return c
}

func tool(t *testing.T, c *metrika.Connector, name string) connectors.Tool {
	t.Helper()
	for _, item := range c.Tools() {
		if item.Name == name {
			return item
		}
	}
	t.Fatalf("tool %q not found", name)
	return connectors.Tool{}
}

func env() connectors.Env {
	return connectors.Env{AccessToken: "yandex-token", Client: &http.Client{}}
}

func decodeResult(t *testing.T, payload any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	return out
}

func TestTools(t *testing.T) {
	c := metrika.New()
	tools := c.Tools()
	if len(tools) != 5 {
		t.Fatalf("tools: want 5, got %d", len(tools))
	}
	want := map[string]bool{
		"counters":        false,
		"table":           false,
		"bytime":          false,
		"goals":           false,
		"traffic_sources": false,
	}
	for _, item := range tools {
		if _, ok := want[item.Name]; !ok {
			t.Errorf("unexpected tool %q", item.Name)
			continue
		}
		want[item.Name] = true
		if item.Description == "" {
			t.Errorf("tool %q: empty description", item.Name)
		}
		if item.InputSchema["type"] != "object" {
			t.Errorf("tool %q: schema type %v", item.Name, item.InputSchema["type"])
		}
		if item.Call == nil {
			t.Errorf("tool %q: no handler", item.Name)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("tool %q missing", name)
		}
	}
}

func TestCounters(t *testing.T) {
	s := newStub(t, `{"counters":[{"id":123456,"name":"Мой сайт","site":"example.com","status":"Active","permission":"own"}]}`)
	payload, err := tool(t, s.connector(), "counters").Call(context.Background(), env(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("counters: %v", err)
	}
	if s.lastPath != "/management/v1/counters" {
		t.Errorf("path: want /management/v1/counters, got %s", s.lastPath)
	}
	if got := s.last.Header.Get("Authorization"); got != "OAuth yandex-token" {
		t.Errorf("authorization: %q", got)
	}
	out := decodeResult(t, payload)
	counters, _ := out["counters"].([]any)
	if len(counters) != 1 {
		t.Fatalf("counters: %v", out["counters"])
	}
	first, _ := counters[0].(map[string]any)
	if first["id"] != float64(123456) || first["name"] != "Мой сайт" || first["site"] != "example.com" {
		t.Errorf("counter: %v", first)
	}
}

func TestTable(t *testing.T) {
	s := newStub(t, `{"total_rows":1,"totals":[10,5],"data":[{"dimensions":[{"name":"ym:s:lastsignTrafficSource","value":"organic"}],"metrics":[10,5]}]}`)
	c := s.connector()
	args := json.RawMessage(`{
		"counter_id": 123456,
		"metrics": ["ym:s:visits","ym:s:users"],
		"dimensions": ["ym:s:lastsignTrafficSource"],
		"date1": "2026-11-01",
		"date2": "2026-11-15",
		"filters": "ym:s:deviceCategory=='mobile'",
		"sort": "-ym:s:visits",
		"limit": 50,
		"accuracy": "full"
	}`)
	payload, err := tool(t, c, "table").Call(context.Background(), env(), args)
	if err != nil {
		t.Fatalf("table: %v", err)
	}
	if s.lastPath != "/stat/v1/data" {
		t.Errorf("path: %s", s.lastPath)
	}
	query := s.last.URL.Query()
	for name, wantValue := range map[string]string{
		"ids":        "123456",
		"metrics":    "ym:s:visits,ym:s:users",
		"dimensions": "ym:s:lastsignTrafficSource",
		"date1":      "2026-11-01",
		"date2":      "2026-11-15",
		"filters":    "ym:s:deviceCategory=='mobile'",
		"sort":       "-ym:s:visits",
		"limit":      "50",
		"accuracy":   "full",
	} {
		if got := query.Get(name); got != wantValue {
			t.Errorf("query %s: want %q, got %q", name, wantValue, got)
		}
	}
	// The report body is passed to the assistant as is.
	body, ok := payload.(json.RawMessage)
	if !ok {
		t.Fatalf("payload type: %T", payload)
	}
	if !strings.Contains(string(body), `"total_rows":1`) {
		t.Errorf("payload: %s", body)
	}
}

func TestTableDefaults(t *testing.T) {
	s := newStub(t, `{"total_rows":0,"data":[]}`)
	_, err := tool(t, s.connector(), "table").Call(context.Background(), env(), json.RawMessage(`{"counter_id":42,"metrics":["ym:s:visits"]}`))
	if err != nil {
		t.Fatalf("table: %v", err)
	}
	query := s.last.URL.Query()
	if got := query.Get("date1"); got != "7daysAgo" {
		t.Errorf("default date1: %q", got)
	}
	if got := query.Get("date2"); got != "today" {
		t.Errorf("default date2: %q", got)
	}
	if got := query.Get("limit"); got != "100" {
		t.Errorf("default limit: %q", got)
	}
}

func TestBytime(t *testing.T) {
	s := newStub(t, `{"total_rows":2,"time_intervals":[["2026-11-01","2026-11-02"]],"data":[]}`)
	args := json.RawMessage(`{"counter_id":7,"metrics":["ym:s:visits"],"group":"week"}`)
	if _, err := tool(t, s.connector(), "bytime").Call(context.Background(), env(), args); err != nil {
		t.Fatalf("bytime: %v", err)
	}
	if s.lastPath != "/stat/v1/data/bytime" {
		t.Errorf("path: %s", s.lastPath)
	}
	if got := s.last.URL.Query().Get("group"); got != "week" {
		t.Errorf("group: %q", got)
	}
}

func TestGoals(t *testing.T) {
	s := newStub(t, `{"goals":[{"id":1,"name":"Заказ","type":"action","is_favorite":true}]}`)
	payload, err := tool(t, s.connector(), "goals").Call(context.Background(), env(), json.RawMessage(`{"counter_id":123456}`))
	if err != nil {
		t.Fatalf("goals: %v", err)
	}
	if s.lastPath != "/management/v1/counter/123456/goals" {
		t.Errorf("path: %s", s.lastPath)
	}
	out := decodeResult(t, payload)
	goals, _ := out["goals"].([]any)
	if len(goals) != 1 {
		t.Fatalf("goals: %v", out["goals"])
	}
	if goal := goals[0].(map[string]any); goal["name"] != "Заказ" || goal["type"] != "action" {
		t.Errorf("goal: %v", goal)
	}
}

func TestTrafficSources(t *testing.T) {
	s := newStub(t, `{"total_rows":1,"data":[]}`)
	if _, err := tool(t, s.connector(), "traffic_sources").Call(context.Background(), env(), json.RawMessage(`{"counter_id":5,"attribution":"last"}`)); err != nil {
		t.Fatalf("traffic_sources: %v", err)
	}
	query := s.last.URL.Query()
	if got := query.Get("dimensions"); got != "ym:s:lastsignTrafficSource" {
		t.Errorf("dimensions: %q", got)
	}
	if got := query.Get("metrics"); !strings.Contains(got, "ym:s:visits") {
		t.Errorf("metrics: %q", got)
	}
	if got := query.Get("attribution"); got != "last" {
		t.Errorf("attribution: %q", got)
	}
	if got := query.Get("sort"); got != "-ym:s:visits" {
		t.Errorf("sort: %q", got)
	}
}

func TestAPIError(t *testing.T) {
	s := newStub(t, `{"errors":[{"error_type":"invalid_token","message":"Invalid oauth_token"}]}`)
	s.status = http.StatusForbidden
	_, err := tool(t, s.connector(), "counters").Call(context.Background(), env(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "Invalid oauth_token") {
		t.Errorf("error: %v", err)
	}
}

func TestValidation(t *testing.T) {
	s := newStub(t, `{}`)
	c := s.connector()
	cases := []struct {
		name string
		args string
		want string
	}{
		{"table without counter", `{"metrics":["ym:s:visits"]}`, "counter_id"},
		{"table without metrics", `{"counter_id":1,"metrics":[]}`, "метрик"},
		{"bytime without counter", `{}`, "counter_id"},
		{"goals without counter", `{}`, "counter_id"},
		{"traffic_sources without counter", `{}`, "counter_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			toolName := strings.Fields(tc.name)[0]
			_, err := tool(t, c, toolName).Call(context.Background(), env(), json.RawMessage(tc.args))
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error: %v, want %q", err, tc.want)
			}
		})
	}
}

func TestMissingToken(t *testing.T) {
	s := newStub(t, `{}`)
	_, err := tool(t, s.connector(), "counters").Call(context.Background(), connectors.Env{}, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("want an error")
	}
	if s.last != nil {
		t.Errorf("no request expected, got %s", s.lastPath)
	}
}
