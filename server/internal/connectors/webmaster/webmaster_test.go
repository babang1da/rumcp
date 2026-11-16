package webmaster_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/babang1da/rumcp/server/internal/connectors"
	"github.com/babang1da/rumcp/server/internal/connectors/webmaster"
)

// hostID is the id Yandex gives a site: protocol, host and port, no slashes.
const hostID = "https:example.com:443"

// stub is a fake Webmaster API: it records the requests it gets and answers by
// path with a canned body.
type stub struct {
	server   *httptest.Server
	paths    []string
	last     *http.Request
	status   int
	bodies   map[string]string
	fallback string
}

func newStub(t *testing.T, fallback string) *stub {
	t.Helper()
	s := &stub{
		status:   http.StatusOK,
		fallback: fallback,
		// Every tool resolves the user id first.
		bodies: map[string]string{"/user": `{"user_id":12345,"login":"seo"}`},
	}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.paths = append(s.paths, r.URL.Path)
		s.last = r.Clone(r.Context())
		body, ok := s.bodies[r.URL.Path]
		if !ok {
			body = s.fallback
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s.status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *stub) connector() *webmaster.Connector {
	c := webmaster.New()
	c.BaseURL = s.server.URL
	return c
}

// body sets the answer of one path.
func (s *stub) body(path, body string) {
	s.bodies[path] = body
}

func (s *stub) reset() {
	s.paths = nil
	s.last = nil
}

func tool(t *testing.T, c *webmaster.Connector, name string) connectors.Tool {
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
	c := webmaster.New()
	if c.Name() != "webmaster" {
		t.Errorf("name: %q", c.Name())
	}
	tools := c.Tools()
	if len(tools) != 5 {
		t.Fatalf("tools: want 5, got %d", len(tools))
	}
	want := map[string]bool{
		"sites":          false,
		"indexing":       false,
		"search_queries": false,
		"diagnostics":    false,
		"sitemaps":       false,
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

func TestSites(t *testing.T) {
	s := newStub(t, `{}`)
	s.body("/user/12345/hosts", `{"hosts":[{"host_id":"https:example.com:443","ascii_host_url":"https://example.com/","unicode_host_url":"https://пример.рф/","verified":true,"host_data_status":"OK","host_display_name":"пример.рф"}]}`)
	payload, err := tool(t, s.connector(), "sites").Call(context.Background(), env(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("sites: %v", err)
	}
	if len(s.paths) != 2 || s.paths[0] != "/user" || s.paths[1] != "/user/12345/hosts" {
		t.Errorf("paths: %v, want [/user /user/12345/hosts]", s.paths)
	}
	if got := s.last.Header.Get("Authorization"); got != "OAuth yandex-token" {
		t.Errorf("authorization: %q", got)
	}
	out := decodeResult(t, payload)
	sites, _ := out["sites"].([]any)
	if len(sites) != 1 {
		t.Fatalf("sites: %v", out["sites"])
	}
	site, _ := sites[0].(map[string]any)
	if site["host_id"] != hostID {
		t.Errorf("host_id: %v", site["host_id"])
	}
	if site["url"] != "https://пример.рф/" {
		t.Errorf("url: %v", site["url"])
	}
	if site["name"] != "пример.рф" {
		t.Errorf("name: %v", site["name"])
	}
	if site["verified"] != true || site["status"] != "OK" {
		t.Errorf("site: %v", site)
	}
}

func TestIndexing(t *testing.T) {
	s := newStub(t, `{}`)
	summary := `{"summary":{"sqi":20,"searchable_pages_count":1234,"excluded_pages_count":12,"site_problems":{"critical":1,"non_critical":2}}}`
	s.body("/user/12345/hosts/"+hostID+"/summary", summary)
	payload, err := tool(t, s.connector(), "indexing").Call(context.Background(), env(), json.RawMessage(`{"host_id":"`+hostID+`"}`))
	if err != nil {
		t.Fatalf("indexing: %v", err)
	}
	if got := s.paths[len(s.paths)-1]; got != "/user/12345/hosts/"+hostID+"/summary" {
		t.Errorf("path: %s", got)
	}
	// Yandex documents the site id percent-encoded on the wire.
	if got := s.last.URL.EscapedPath(); got != "/user/12345/hosts/https%3Aexample.com%3A443/summary" {
		t.Errorf("escaped path: %s", got)
	}
	// The summary is passed to the assistant as is.
	body, ok := payload.(json.RawMessage)
	if !ok {
		t.Fatalf("payload type: %T", payload)
	}
	if string(body) != summary {
		t.Errorf("payload: %s", body)
	}
}

func TestSearchQueries(t *testing.T) {
	s := newStub(t, `{"queries":[{"query_id":"1","query_text":"купить слона","indicators":{"TOTAL_SHOWS":10}}]}`)
	args := json.RawMessage(`{
		"host_id": "` + hostID + `",
		"order_by": "TOTAL_CLICKS",
		"query_indicator": "AVG_SHOW_POSITION",
		"limit": 20,
		"offset": 40
	}`)
	payload, err := tool(t, s.connector(), "search_queries").Call(context.Background(), env(), args)
	if err != nil {
		t.Fatalf("search_queries: %v", err)
	}
	if got := s.paths[len(s.paths)-1]; got != "/user/12345/hosts/"+hostID+"/search-queries/popular" {
		t.Errorf("path: %s", got)
	}
	query := s.last.URL.Query()
	for name, wantValue := range map[string]string{
		"order_by":        "TOTAL_CLICKS",
		"query_indicator": "AVG_SHOW_POSITION",
		"limit":           "20",
		"offset":          "40",
	} {
		if got := query.Get(name); got != wantValue {
			t.Errorf("query %s: want %q, got %q", name, wantValue, got)
		}
	}
	body, ok := payload.(json.RawMessage)
	if !ok {
		t.Fatalf("payload type: %T", payload)
	}
	if !strings.Contains(string(body), "купить слона") {
		t.Errorf("payload: %s", body)
	}
}

func TestSearchQueriesDefaults(t *testing.T) {
	s := newStub(t, `{"queries":[]}`)
	if _, err := tool(t, s.connector(), "search_queries").Call(context.Background(), env(), json.RawMessage(`{"host_id":"`+hostID+`"}`)); err != nil {
		t.Fatalf("search_queries: %v", err)
	}
	query := s.last.URL.Query()
	if got := query.Get("order_by"); got != "TOTAL_SHOWS" {
		t.Errorf("default order_by: %q", got)
	}
	if got := query.Get("query_indicator"); got != "TOTAL_SHOWS" {
		t.Errorf("default query_indicator: %q", got)
	}
	if got := query.Get("limit"); got != "100" {
		t.Errorf("default limit: %q", got)
	}
	if got := query.Get("offset"); got != "0" {
		t.Errorf("default offset: %q", got)
	}
}

func TestDiagnostics(t *testing.T) {
	s := newStub(t, `{"problems":[{"problem_type":"NO_ROBOTS_TXT","severity":"RECOMMENDATION","state":"PRESENT"}]}`)
	payload, err := tool(t, s.connector(), "diagnostics").Call(context.Background(), env(), json.RawMessage(`{"host_id":"`+hostID+`"}`))
	if err != nil {
		t.Fatalf("diagnostics: %v", err)
	}
	if got := s.paths[len(s.paths)-1]; got != "/user/12345/hosts/"+hostID+"/diagnostics" {
		t.Errorf("path: %s", got)
	}
	body, ok := payload.(json.RawMessage)
	if !ok {
		t.Fatalf("payload type: %T", payload)
	}
	if !strings.Contains(string(body), "NO_ROBOTS_TXT") {
		t.Errorf("payload: %s", body)
	}
}

func TestSitemaps(t *testing.T) {
	s := newStub(t, `{"sitemaps":[{"sitemap_id":"42","sitemap_url":"https://example.com/sitemap.xml","urls_count":10}]}`)
	args := json.RawMessage(`{"host_id":"` + hostID + `","limit":10,"from":"42"}`)
	payload, err := tool(t, s.connector(), "sitemaps").Call(context.Background(), env(), args)
	if err != nil {
		t.Fatalf("sitemaps: %v", err)
	}
	if got := s.paths[len(s.paths)-1]; got != "/user/12345/hosts/"+hostID+"/sitemaps" {
		t.Errorf("path: %s", got)
	}
	query := s.last.URL.Query()
	if got := query.Get("limit"); got != "10" {
		t.Errorf("limit: %q", got)
	}
	if got := query.Get("from"); got != "42" {
		t.Errorf("from: %q", got)
	}
	body, ok := payload.(json.RawMessage)
	if !ok {
		t.Fatalf("payload type: %T", payload)
	}
	if !strings.Contains(string(body), "sitemap.xml") {
		t.Errorf("payload: %s", body)
	}
}

func TestSitemapsDefaults(t *testing.T) {
	s := newStub(t, `{"sitemaps":[]}`)
	if _, err := tool(t, s.connector(), "sitemaps").Call(context.Background(), env(), json.RawMessage(`{"host_id":"`+hostID+`"}`)); err != nil {
		t.Fatalf("sitemaps: %v", err)
	}
	query := s.last.URL.Query()
	if got := query.Get("limit"); got != "50" {
		t.Errorf("default limit: %q", got)
	}
	if _, ok := query["from"]; ok {
		t.Errorf("from must be absent: %v", query)
	}
}

func TestAPIError(t *testing.T) {
	s := newStub(t, `{}`)
	s.status = http.StatusForbidden
	s.body("/user", `{"error_code":"INVALID_OAUTH_TOKEN","error_message":"Invalid OAuth token"}`)
	_, err := tool(t, s.connector(), "sites").Call(context.Background(), env(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "Invalid OAuth token") {
		t.Errorf("error: %v", err)
	}
}

func TestValidation(t *testing.T) {
	s := newStub(t, `{}`)
	c := s.connector()
	for _, name := range []string{"indexing", "search_queries", "diagnostics", "sitemaps"} {
		t.Run(name, func(t *testing.T) {
			s.reset()
			_, err := tool(t, c, name).Call(context.Background(), env(), json.RawMessage(`{}`))
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), "host_id") {
				t.Errorf("error: %v, want host_id", err)
			}
			if len(s.paths) != 0 {
				t.Errorf("no request expected, got %v", s.paths)
			}
		})
	}
}

func TestMissingToken(t *testing.T) {
	s := newStub(t, `{}`)
	_, err := tool(t, s.connector(), "sites").Call(context.Background(), connectors.Env{}, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "токена Яндекса") {
		t.Errorf("error: %v", err)
	}
	if len(s.paths) != 0 {
		t.Errorf("no request expected, got %v", s.paths)
	}
}

func TestNoUserID(t *testing.T) {
	s := newStub(t, `{}`)
	s.body("/user", `{"login":"seo"}`)
	_, err := tool(t, s.connector(), "sites").Call(context.Background(), env(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "user_id") {
		t.Errorf("error: %v", err)
	}
}
