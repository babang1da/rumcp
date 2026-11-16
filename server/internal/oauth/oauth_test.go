package oauth_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/babang1da/rumcp/server/internal/auth"
	"github.com/babang1da/rumcp/server/internal/oauth"
	"github.com/babang1da/rumcp/server/internal/store"
	"github.com/babang1da/rumcp/server/internal/store/storetest"
)

const (
	issuer      = "http://localhost:8080"
	redirectURI = "http://localhost:9999/callback"
)

type env struct {
	t       *testing.T
	repo    *storetest.Memory
	session *auth.Session
	server  *httptest.Server
	client  *http.Client
	user    store.User
}

func newEnv(t *testing.T) *env {
	t.Helper()
	repo := storetest.New()
	user, err := repo.UpsertUser(t.Context(), store.User{
		YandexID: "100500", Login: "egor", Email: "egor@example.com", Name: "Egor",
	})
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	session := auth.NewSession([]byte("test-session-secret-long-enough"), false)
	cfg := oauth.DefaultConfig(issuer)
	cfg.CSRFKey = []byte("consent-key-for-tests-0123456789")
	handler := &oauth.Handler{
		Repo:    repo,
		Session: session,
		Cfg:     cfg,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth/register", handler.Register)
	mux.HandleFunc("/oauth/authorize", handler.Authorize)
	mux.HandleFunc("POST /oauth/token", handler.Token)
	mux.Handle("GET /protected", handler.RequireBearer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, _ := oauth.UserIDFromContext(r.Context())
		_, _ = io.WriteString(w, userID)
	})))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &env{
		t:       t,
		repo:    repo,
		session: session,
		server:  srv,
		client:  &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		user:    user,
	}
}

// sessionCookie returns the cookie a signed-in cabinet request carries.
func (e *env) sessionCookie() *http.Cookie {
	recorder := httptest.NewRecorder()
	e.session.Set(recorder, e.user.ID)
	cookies := recorder.Result().Cookies()
	if len(cookies) == 0 {
		e.t.Fatal("session cookie was not set")
	}
	return cookies[0]
}

func (e *env) do(req *http.Request) *http.Response {
	e.t.Helper()
	resp, err := e.client.Do(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", req.Method, req.URL, err)
	}
	e.t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// registerClient performs dynamic client registration and returns the response.
func (e *env) registerClient(authMethod string) map[string]any {
	e.t.Helper()
	payload := map[string]any{
		"client_name":                "Claude",
		"redirect_uris":              []string{redirectURI},
		"token_endpoint_auth_method": authMethod,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		e.t.Fatalf("marshal: %v", err)
	}
	req, _ := http.NewRequest(http.MethodPost, e.server.URL+"/oauth/register", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	resp := e.do(req)
	if resp.StatusCode != http.StatusCreated {
		e.t.Fatalf("register status %d, want 201", resp.StatusCode)
	}
	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		e.t.Fatalf("register decode: %v", err)
	}
	return decoded
}

// authorize asks for an authorization code; withSession adds the cabinet cookie.
func (e *env) authorize(params url.Values, withSession bool) *http.Response {
	e.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, e.server.URL+"/oauth/authorize?"+params.Encode(), nil)
	if withSession {
		req.AddCookie(e.sessionCookie())
	}
	return e.do(req)
}

// consent runs the consent screen of the cabinet user and returns the answer.
func (e *env) consent(params url.Values, withSession bool, decision string) *http.Response {
	e.t.Helper()
	page := e.authorize(params, withSession)
	if page.StatusCode != http.StatusOK {
		e.t.Fatalf("consent page status %d, want 200", page.StatusCode)
	}
	return e.submitConsent(page, withSession, decision)
}

// submitConsent posts the consent form of a rendered page.
func (e *env) submitConsent(page *http.Response, withSession bool, decision string) *http.Response {
	e.t.Helper()
	body, err := io.ReadAll(page.Body)
	if err != nil {
		e.t.Fatalf("read consent page: %v", err)
	}
	form := parseConsentForm(e.t, string(body))
	form.Set("decision", decision)
	return e.postForm("/oauth/authorize", form, withSession)
}

func (e *env) postForm(path string, form url.Values, withSession bool) *http.Response {
	e.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, e.server.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if withSession {
		req.AddCookie(e.sessionCookie())
	}
	return e.do(req)
}

var formInput = regexp.MustCompile(`name="([^"]+)" value="([^"]*)"`)

// parseConsentForm reads the hidden inputs of the consent page.
func parseConsentForm(t *testing.T, page string) url.Values {
	t.Helper()
	form := url.Values{}
	for _, match := range formInput.FindAllStringSubmatch(page, -1) {
		form.Set(match[1], html.UnescapeString(match[2]))
	}
	if form.Get("csrf") == "" {
		t.Fatalf("consent page carries no csrf token")
	}
	return form
}

const verifier = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ-._~"

func challengeOf(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// hashForTest is the SHA-256 hex hash the service stores instead of a token.
func hashForTest(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func authorizeParams(clientID string) url.Values {
	return url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"state":                 {"xyz"},
		"scope":                 {"mcp"},
		"code_challenge":        {challengeOf(verifier)},
		"code_challenge_method": {"S256"},
	}
}

func codeFromRedirect(t *testing.T, resp *http.Response) string {
	t.Helper()
	location := resp.Header.Get("Location")
	if location == "" {
		t.Fatalf("authorize status %d, no Location", resp.StatusCode)
	}
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse Location %q: %v", location, err)
	}
	if got := parsed.Query().Get("state"); got != "xyz" {
		t.Fatalf("state = %q, want xyz", got)
	}
	if errCode := parsed.Query().Get("error"); errCode != "" {
		t.Fatalf("authorize returned error %q (%s)", errCode, parsed.Query().Get("error_description"))
	}
	code := parsed.Query().Get("code")
	if code == "" {
		t.Fatalf("no code in Location %q", location)
	}
	return code
}

func (e *env) exchange(form url.Values) (*http.Response, map[string]any) {
	e.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, e.server.URL+"/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := e.do(req)
	var decoded map[string]any
	body, _ := io.ReadAll(resp.Body)
	if len(body) > 0 {
		_ = json.Unmarshal(body, &decoded)
	}
	return resp, decoded
}

func TestAuthorizationCodeFlowWithPKCE(t *testing.T) {
	e := newEnv(t)
	client := e.registerClient("none")
	clientID := client["client_id"].(string)

	// The client cannot get a code without an explicit consent of the user.
	page := e.authorize(authorizeParams(clientID), true)
	if page.StatusCode != http.StatusOK {
		t.Fatalf("consent page status %d, want 200", page.StatusCode)
	}
	if got := page.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want DENY on the consent page", got)
	}
	decided := e.submitConsent(page, true, "allow")
	if decided.StatusCode != http.StatusFound {
		t.Fatalf("consent decision status %d, want 302", decided.StatusCode)
	}
	code := codeFromRedirect(t, decided)

	resp, payload := e.exchange(url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token status %d, want 200: %v", resp.StatusCode, payload)
	}
	accessToken, _ := payload["access_token"].(string)
	if accessToken == "" {
		t.Fatalf("no access_token in %v", payload)
	}
	if payload["token_type"] != "Bearer" {
		t.Errorf("token_type = %v, want Bearer", payload["token_type"])
	}
	if payload["refresh_token"] == "" {
		t.Errorf("no refresh_token in %v", payload)
	}

	// The access token authorizes a protected request as the signed-in user.
	req, _ := http.NewRequest(http.MethodGet, e.server.URL+"/protected", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	protected := e.do(req)
	if protected.StatusCode != http.StatusOK {
		t.Fatalf("protected status %d, want 200", protected.StatusCode)
	}
	body, _ := io.ReadAll(protected.Body)
	if string(body) != e.user.ID {
		t.Fatalf("protected returned user %q, want %q", body, e.user.ID)
	}

	// Only the hash of the token is stored.
	stored, err := e.repo.TokenByHash(t.Context(), hashForTest(accessToken), store.TokenKindAccess)
	if err != nil {
		t.Fatalf("access token was not found by hash: %v", err)
	}
	if stored.TokenHash == accessToken {
		t.Fatal("access token is stored in plaintext")
	}
}

func TestConsentPageNamesTheClient(t *testing.T) {
	e := newEnv(t)
	clientID := e.registerClient("none")["client_id"].(string)

	page := e.authorize(authorizeParams(clientID), true)
	if page.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", page.StatusCode)
	}
	body, err := io.ReadAll(page.Body)
	if err != nil {
		t.Fatalf("read page: %v", err)
	}
	if !strings.Contains(string(body), "Claude") {
		t.Fatal("consent page does not name the client")
	}
	if strings.Contains(string(body), `name="decision" value="allow" disabled`) {
		t.Fatal("consent page has no approval control")
	}
	if page.Header.Get("Content-Security-Policy") == "" {
		t.Error("consent page has no Content-Security-Policy")
	}
}

func TestConsentDeniedIssuesNoCode(t *testing.T) {
	e := newEnv(t)
	clientID := e.registerClient("none")["client_id"].(string)

	resp := e.consent(authorizeParams(clientID), true, "deny")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status %d, want 302", resp.StatusCode)
	}
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if got := location.Query().Get("error"); got != "access_denied" {
		t.Fatalf("error = %q, want access_denied", got)
	}
	if got := location.Query().Get("code"); got != "" {
		t.Fatalf("code %q was issued after a denial", got)
	}
	if got := location.Query().Get("state"); got != "xyz" {
		t.Fatalf("state = %q, want the client state back", got)
	}
}

func TestConsentRejectsForgedCSRF(t *testing.T) {
	e := newEnv(t)
	clientID := e.registerClient("none")["client_id"].(string)

	page := e.authorize(authorizeParams(clientID), true)
	body, _ := io.ReadAll(page.Body)
	form := parseConsentForm(t, string(body))
	form.Set("decision", "allow")
	form.Set("csrf", "forged-token")

	if resp := e.postForm("/oauth/authorize", form, true); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d, want 403", resp.StatusCode)
	}
}

func TestConsentPostWithoutSession(t *testing.T) {
	e := newEnv(t)
	clientID := e.registerClient("none")["client_id"].(string)

	page := e.authorize(authorizeParams(clientID), true)
	body, _ := io.ReadAll(page.Body)
	form := parseConsentForm(t, string(body))
	form.Set("decision", "allow")

	if resp := e.postForm("/oauth/authorize", form, false); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", resp.StatusCode)
	}
}

func TestConsentPostRevalidatesTheRequest(t *testing.T) {
	e := newEnv(t)
	clientID := e.registerClient("none")["client_id"].(string)

	page := e.authorize(authorizeParams(clientID), true)
	body, _ := io.ReadAll(page.Body)
	form := parseConsentForm(t, string(body))
	form.Set("decision", "allow")
	form.Del("code_challenge")

	resp := e.postForm("/oauth/authorize", form, true)
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if got := location.Query().Get("error"); got != "invalid_request" {
		t.Fatalf("error = %q, want invalid_request", got)
	}
}

func TestAuthorizeWithoutPKCEIsRejected(t *testing.T) {
	e := newEnv(t)
	clientID := e.registerClient("none")["client_id"].(string)

	params := authorizeParams(clientID)
	params.Del("code_challenge")
	params.Del("code_challenge_method")
	resp := e.authorize(params, true)

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status %d, want 302", resp.StatusCode)
	}
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if got := location.Query().Get("error"); got != "invalid_request" {
		t.Fatalf("error = %q, want invalid_request", got)
	}
	if got := location.Query().Get("code"); got != "" {
		t.Fatalf("code %q was issued without PKCE", got)
	}
}

func TestAuthorizeWithPlainPKCEIsRejected(t *testing.T) {
	e := newEnv(t)
	clientID := e.registerClient("none")["client_id"].(string)

	params := authorizeParams(clientID)
	params.Set("code_challenge_method", "plain")
	params.Set("code_challenge", verifier)
	resp := e.authorize(params, true)

	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if got := location.Query().Get("error"); got != "invalid_request" {
		t.Fatalf("error = %q, want invalid_request for plain PKCE", got)
	}
}

func TestAuthorizeWithoutSessionGoesToYandexLogin(t *testing.T) {
	e := newEnv(t)
	clientID := e.registerClient("none")["client_id"].(string)

	resp := e.authorize(authorizeParams(clientID), false)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status %d, want 302", resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	if !strings.HasPrefix(location, "/oauth/yandex/login") {
		t.Fatalf("Location = %q, want the Yandex login", location)
	}
	if !strings.Contains(location, "return_to=") {
		t.Fatalf("Location = %q, want the authorization request to be kept", location)
	}
}

func TestAuthorizeRejectsUnregisteredRedirectURI(t *testing.T) {
	e := newEnv(t)
	clientID := e.registerClient("none")["client_id"].(string)

	params := authorizeParams(clientID)
	params.Set("redirect_uri", "https://evil.example/callback")
	resp := e.authorize(params, true)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", resp.StatusCode)
	}
	if resp.Header.Get("Location") != "" {
		t.Fatalf("request was redirected to an unregistered URI: %q", resp.Header.Get("Location"))
	}
}

func TestAuthorizeRejectsUnknownClient(t *testing.T) {
	e := newEnv(t)
	resp := e.authorize(authorizeParams("rumcp_unknown"), true)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", resp.StatusCode)
	}
}

func TestTokenRejectsWrongVerifier(t *testing.T) {
	e := newEnv(t)
	clientID := e.registerClient("none")["client_id"].(string)
	code := codeFromRedirect(t, e.consent(authorizeParams(clientID), true, "allow"))

	resp, payload := e.exchange(url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"code_verifier": {strings.Repeat("a", 64)},
	})
	if resp.StatusCode != http.StatusBadRequest || payload["error"] != "invalid_grant" {
		t.Fatalf("status %d error %v, want 400 invalid_grant", resp.StatusCode, payload["error"])
	}
}

func TestTokenRejectsMissingVerifier(t *testing.T) {
	e := newEnv(t)
	clientID := e.registerClient("none")["client_id"].(string)
	code := codeFromRedirect(t, e.consent(authorizeParams(clientID), true, "allow"))

	resp, payload := e.exchange(url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"client_id":    {clientID},
		"redirect_uri": {redirectURI},
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", resp.StatusCode)
	}
	if payload["error"] != "invalid_request" {
		t.Fatalf("error = %v, want invalid_request", payload["error"])
	}
}

func TestTokenRejectsReplayedCode(t *testing.T) {
	e := newEnv(t)
	clientID := e.registerClient("none")["client_id"].(string)
	code := codeFromRedirect(t, e.consent(authorizeParams(clientID), true, "allow"))

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	if resp, _ := e.exchange(form); resp.StatusCode != http.StatusOK {
		t.Fatalf("first exchange status %d, want 200", resp.StatusCode)
	}
	resp, payload := e.exchange(form)
	if resp.StatusCode != http.StatusBadRequest || payload["error"] != "invalid_grant" {
		t.Fatalf("second exchange status %d error %v, want 400 invalid_grant", resp.StatusCode, payload["error"])
	}
}

func TestConcurrentCodeExchangeIssuesOneToken(t *testing.T) {
	e := newEnv(t)
	clientID := e.registerClient("none")["client_id"].(string)
	code := codeFromRedirect(t, e.consent(authorizeParams(clientID), true, "allow"))

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}

	const attempts = 4
	statuses := make(chan int, attempts)
	var wg sync.WaitGroup
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodPost, e.server.URL+"/oauth/token", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			resp, err := e.client.Do(req)
			if err != nil {
				statuses <- 0
				return
			}
			defer func() { _ = resp.Body.Close() }()
			_, _ = io.Copy(io.Discard, resp.Body)
			statuses <- resp.StatusCode
		}()
	}
	wg.Wait()
	close(statuses)

	issued := 0
	for status := range statuses {
		switch status {
		case http.StatusOK:
			issued++
		case http.StatusBadRequest:
		default:
			t.Fatalf("unexpected exchange status %d", status)
		}
	}
	if issued != 1 {
		t.Fatalf("%d exchanges issued a token for one code, want exactly 1", issued)
	}
}

func TestRefreshTokenRotation(t *testing.T) {
	e := newEnv(t)
	clientID := e.registerClient("none")["client_id"].(string)
	code := codeFromRedirect(t, e.consent(authorizeParams(clientID), true, "allow"))

	_, first := e.exchange(url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	})
	refresh, _ := first["refresh_token"].(string)
	if refresh == "" {
		t.Fatalf("no refresh_token in %v", first)
	}

	resp, second := e.exchange(url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refresh},
		"client_id":     {clientID},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refresh status %d, want 200: %v", resp.StatusCode, second)
	}
	if second["access_token"] == first["access_token"] {
		t.Fatal("refresh returned the same access token")
	}
	if second["refresh_token"] == refresh {
		t.Fatal("refresh token was not rotated")
	}

	if resp, payload := e.exchange(url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refresh},
		"client_id":     {clientID},
	}); resp.StatusCode != http.StatusBadRequest || payload["error"] != "invalid_grant" {
		t.Fatalf("reused refresh token status %d error %v, want 400 invalid_grant", resp.StatusCode, payload["error"])
	}
}

func TestProtectedEndpointWithoutBearer(t *testing.T) {
	e := newEnv(t)
	req, _ := http.NewRequest(http.MethodGet, e.server.URL+"/protected", nil)
	resp := e.do(req)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", resp.StatusCode)
	}
	challenge := resp.Header.Get("WWW-Authenticate")
	if !strings.Contains(challenge, "Bearer") {
		t.Fatalf("WWW-Authenticate = %q, want a Bearer challenge", challenge)
	}
	if !strings.Contains(challenge, ".well-known/oauth-protected-resource") {
		t.Fatalf("WWW-Authenticate = %q, want resource metadata", challenge)
	}
}

func TestProtectedEndpointRejectsUnknownBearer(t *testing.T) {
	e := newEnv(t)
	req, _ := http.NewRequest(http.MethodGet, e.server.URL+"/protected", nil)
	req.Header.Set("Authorization", "Bearer not-a-token")
	if resp := e.do(req); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", resp.StatusCode)
	}
}

func TestConfidentialClientNeedsSecret(t *testing.T) {
	e := newEnv(t)
	client := e.registerClient("client_secret_post")
	clientID, _ := client["client_id"].(string)
	secret, _ := client["client_secret"].(string)
	if secret == "" {
		t.Fatal("confidential client was registered without a secret")
	}
	code := codeFromRedirect(t, e.consent(authorizeParams(clientID), true, "allow"))

	resp, payload := e.exchange(url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	})
	if resp.StatusCode != http.StatusUnauthorized || payload["error"] != "invalid_client" {
		t.Fatalf("no-secret exchange status %d error %v, want 401 invalid_client", resp.StatusCode, payload["error"])
	}

	resp, payload = e.exchange(url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"client_secret": {secret},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("with-secret exchange status %d, want 200: %v", resp.StatusCode, payload)
	}
}

func TestRegisterRejectsBadRedirectURI(t *testing.T) {
	e := newEnv(t)
	for _, uri := range []string{"javascript:alert(1)", "/relative", "https://example.com/cb#fragment", "https://user@example.com/cb"} {
		payload, _ := json.Marshal(map[string]any{"redirect_uris": []string{uri}})
		req, _ := http.NewRequest(http.MethodPost, e.server.URL+"/oauth/register", strings.NewReader(string(payload)))
		req.Header.Set("Content-Type", "application/json")
		resp := e.do(req)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("redirect_uri %q: status %d, want 400", uri, resp.StatusCode)
		}
	}
}

func TestMetadata(t *testing.T) {
	handler := &oauth.Handler{Repo: storetest.New(), Cfg: oauth.DefaultConfig(issuer)}
	recorder := httptest.NewRecorder()
	handler.Metadata(recorder, httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", recorder.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["issuer"] != issuer {
		t.Errorf("issuer = %v, want %s", payload["issuer"], issuer)
	}
	if payload["authorization_endpoint"] != issuer+"/oauth/authorize" {
		t.Errorf("authorization_endpoint = %v", payload["authorization_endpoint"])
	}
	if payload["token_endpoint"] != issuer+"/oauth/token" {
		t.Errorf("token_endpoint = %v", payload["token_endpoint"])
	}
	if payload["registration_endpoint"] != issuer+"/oauth/register" {
		t.Errorf("registration_endpoint = %v", payload["registration_endpoint"])
	}
	methods, _ := payload["code_challenge_methods_supported"].([]any)
	if len(methods) != 1 || methods[0] != "S256" {
		t.Errorf("code_challenge_methods_supported = %v, want [S256]", methods)
	}
}
