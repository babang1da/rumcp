package httpapi_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/babang1da/rumcp/server/internal/auth"
	"github.com/babang1da/rumcp/server/internal/config"
	"github.com/babang1da/rumcp/server/internal/httpapi"
	"github.com/babang1da/rumcp/server/internal/store/storetest"
)

const (
	baseURL     = "http://localhost:8080"
	redirectURI = "http://localhost:9999/callback"
	testToken   = "yandex-test-token"
	verifier    = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ-._~"
)

type fakeYandex struct{}

func (fakeYandex) Exchange(_ context.Context, code, _ string) (auth.YandexToken, error) {
	if code == "" {
		return auth.YandexToken{}, errors.New("no code")
	}
	return auth.YandexToken{AccessToken: testToken, RefreshToken: "refresh", ExpiresIn: 3600, Scope: "metrika:read"}, nil
}

func (fakeYandex) Profile(_ context.Context, accessToken string) (auth.YandexProfile, error) {
	if accessToken != testToken {
		return auth.YandexProfile{}, errors.New("unexpected token")
	}
	return auth.YandexProfile{ID: "100500", Login: "egor", Email: "egor@example.com", Name: "Egor"}, nil
}

func newTestServer(t *testing.T) (*httptest.Server, *storetest.Memory) {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	cfg := config.Config{
		HTTPAddr:        ":8080",
		BaseURL:         baseURL,
		TokenKey:        key,
		SessionSecret:   []byte("session-secret-long-enough"),
		YandexTestToken: testToken,
		DailyLimit:      100,
	}
	repo := storetest.New()
	handler, err := httpapi.NewRouterWithYandex(cfg, repo, fakeYandex{})
	if err != nil {
		t.Fatalf("httpapi.NewRouterWithYandex: %v", err)
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, repo
}

func get(t *testing.T, srv *httptest.Server, path string, cookies ...*http.Cookie) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func postJSON(t *testing.T, srv *httptest.Server, path, body string, cookies ...*http.Cookie) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// postForm submits a form as a browser would.
func postForm(t *testing.T, srv *httptest.Server, path string, form url.Values, cookies ...*http.Cookie) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// parseConsentForm reads the hidden fields of the consent page.
func parseConsentForm(t *testing.T, page *http.Response) url.Values {
	t.Helper()
	body, err := io.ReadAll(page.Body)
	if err != nil {
		t.Fatalf("read consent page: %v", err)
	}
	form := url.Values{}
	for _, match := range regexp.MustCompile(`name="([^"]+)" value="([^"]*)"`).FindAllStringSubmatch(string(body), -1) {
		form.Set(match[1], html.UnescapeString(match[2]))
	}
	if form.Get("csrf") == "" {
		t.Fatal("consent page carries no csrf token")
	}
	return form
}

func TestHealthz(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := get(t, srv, "/healthz")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
}

func TestWellKnownEndpoints(t *testing.T) {
	srv, _ := newTestServer(t)

	resp := get(t, srv, "/.well-known/oauth-authorization-server")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authorization server metadata status %d, want 200", resp.StatusCode)
	}
	var metadata map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if metadata["issuer"] != baseURL {
		t.Errorf("issuer = %v, want %s", metadata["issuer"], baseURL)
	}
	if metadata["token_endpoint"] != baseURL+"/oauth/token" {
		t.Errorf("token_endpoint = %v", metadata["token_endpoint"])
	}

	resp = get(t, srv, "/.well-known/oauth-protected-resource")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("protected resource metadata status %d, want 200", resp.StatusCode)
	}
	var resource map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&resource); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resource["resource"] != baseURL+"/mcp" {
		t.Errorf("resource = %v, want %s/mcp", resource["resource"], baseURL)
	}
}

func TestCabinetRequiresLogin(t *testing.T) {
	srv, _ := newTestServer(t)
	if resp := get(t, srv, "/api/me"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", resp.StatusCode)
	}
}

// TestMCPClientConnectsEndToEnd walks the whole path an MCP client takes:
// Yandex login, dynamic registration, authorization with PKCE, token, cabinet.
func TestMCPClientConnectsEndToEnd(t *testing.T) {
	srv, repo := newTestServer(t)

	// 1. The user signs in through Yandex (dev token while there is no app).
	login := get(t, srv, "/oauth/yandex/login")
	if login.StatusCode != http.StatusFound {
		t.Fatalf("login status %d, want 302", login.StatusCode)
	}
	var session *http.Cookie
	for _, cookie := range login.Cookies() {
		if cookie.Name == "rumcp_session" {
			session = cookie
		}
	}
	if session == nil {
		t.Fatal("no cabinet session after login")
	}

	// The token of the user is stored sealed, never in plaintext.
	users := repo.Users()
	if len(users) != 1 {
		t.Fatalf("%d users, want 1", len(users))
	}
	sealed, ok := repo.AccountTokenEnc(users[0].ID, "yandex")
	if !ok || strings.Contains(sealed, testToken) {
		t.Fatalf("account token stored as %q, want a sealed value", sealed)
	}

	// 2. The MCP client registers itself.
	register := postJSON(t, srv, "/oauth/register",
		`{"client_name":"Claude","redirect_uris":["`+redirectURI+`"],"token_endpoint_auth_method":"none"}`)
	if register.StatusCode != http.StatusCreated {
		t.Fatalf("register status %d, want 201", register.StatusCode)
	}
	var client map[string]any
	if err := json.NewDecoder(register.Body).Decode(&client); err != nil {
		t.Fatalf("register decode: %v", err)
	}
	clientID, _ := client["client_id"].(string)
	if clientID == "" {
		t.Fatal("no client_id in the registration response")
	}

	// 3. The browser part of the flow asks for a code with PKCE and the user
	// confirms the client on the consent screen.
	sum := sha256.Sum256([]byte(verifier))
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"scope":                 {"mcp"},
		"state":                 {"state-1"},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
	}
	consent := get(t, srv, "/oauth/authorize?"+params.Encode(), session)
	if consent.StatusCode != http.StatusOK {
		t.Fatalf("consent page status %d, want 200", consent.StatusCode)
	}
	form := parseConsentForm(t, consent)
	form.Set("decision", "allow")
	authorize := postForm(t, srv, "/oauth/authorize", form, session)
	if authorize.StatusCode != http.StatusFound {
		t.Fatalf("consent decision status %d, want 302", authorize.StatusCode)
	}
	location, err := url.Parse(authorize.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	code := location.Query().Get("code")
	if code == "" || location.Query().Get("error") != "" {
		t.Fatalf("no authorization code in %q", authorize.Header.Get("Location"))
	}

	// 4. The client exchanges the code for a Bearer token.
	tokenForm := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	resp, err := http.PostForm(srv.URL+"/oauth/token", tokenForm)
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("token status %d, want 200: %s", resp.StatusCode, body)
	}
	var token map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		t.Fatalf("token decode: %v", err)
	}
	if token["access_token"] == "" || token["token_type"] != "Bearer" {
		t.Fatalf("token response = %v", token)
	}

	// 5. The cabinet shows the connected account and the usage of the user.
	me := get(t, srv, "/api/me", session)
	if me.StatusCode != http.StatusOK {
		t.Fatalf("api/me status %d, want 200", me.StatusCode)
	}
	var cabinet map[string]any
	if err := json.NewDecoder(me.Body).Decode(&cabinet); err != nil {
		t.Fatalf("api/me decode: %v", err)
	}
	account, _ := cabinet["account"].(map[string]any)
	if account["connected"] != true {
		t.Fatalf("account = %v, want connected", account)
	}
	user, _ := cabinet["user"].(map[string]any)
	if user["login"] != "dev" {
		t.Fatalf("user = %v, want the dev profile of YANDEX_TEST_TOKEN", user)
	}

	// 6. A lead for a connector that is not released yet lands in the store.
	if resp := postJSON(t, srv, "/api/leads", `{"email":"buyer@example.com","service":"direct"}`); resp.StatusCode != http.StatusCreated {
		t.Fatalf("leads status %d, want 201", resp.StatusCode)
	}
	if leads := repo.Leads(); len(leads) != 1 {
		t.Fatalf("%d leads stored, want 1", len(leads))
	}
}

func TestUnknownPathsReturn404(t *testing.T) {
	srv, _ := newTestServer(t)
	if resp := get(t, srv, "/api/unknown"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d, want 404", resp.StatusCode)
	}
}
