package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/babang1da/rumcp/server/internal/auth"
	"github.com/babang1da/rumcp/server/internal/secret"
	"github.com/babang1da/rumcp/server/internal/store/storetest"
)

const testToken = "yandex-test-access-token"

type fakeYandex struct {
	token       auth.YandexToken
	profile     auth.YandexProfile
	profileErr  error
	gotToken    string
	gotCode     string
	gotRedirect string
}

func (f *fakeYandex) Exchange(_ context.Context, code, redirectURI string) (auth.YandexToken, error) {
	f.gotCode = code
	f.gotRedirect = redirectURI
	if f.token.AccessToken == "" {
		return auth.YandexToken{}, errors.New("exchange failed")
	}
	return f.token, nil
}

func (f *fakeYandex) Profile(_ context.Context, accessToken string) (auth.YandexProfile, error) {
	f.gotToken = accessToken
	if f.profileErr != nil {
		return auth.YandexProfile{}, f.profileErr
	}
	return f.profile, nil
}

type fixture struct {
	repo    *storetest.Memory
	handler *auth.Handler
	session *auth.Session
	box     *secret.Box
	yandex  *fakeYandex
}

func newFixture(t *testing.T, devLogin bool) *fixture {
	t.Helper()
	box, err := secret.NewBox([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewBox: %v", err)
	}
	yandex := &fakeYandex{
		token:   auth.YandexToken{AccessToken: testToken, RefreshToken: "refresh-token", ExpiresIn: 3600, Scope: "metrika:read"},
		profile: auth.YandexProfile{ID: "100500", Login: "egor", Email: "egor@example.com", Name: "Egor"},
	}
	session := auth.NewSession([]byte("session-secret-long-enough"), false)
	repo := storetest.New()
	return &fixture{
		repo: repo,
		handler: &auth.Handler{
			Repo:    repo,
			Yandex:  yandex,
			Session: session,
			Box:     box,
			Cfg: auth.Config{
				BaseURL:   "http://localhost:8080",
				ClientID:  "app-id",
				DevLogin:  devLogin,
				TestToken: testToken,
			},
		},
		session: session,
		box:     box,
		yandex:  yandex,
	}
}

func cookieNamed(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func TestDevLoginSealsYandexToken(t *testing.T) {
	f := newFixture(t, true)
	recorder := httptest.NewRecorder()
	f.handler.Login(recorder, httptest.NewRequest(http.MethodGet, "/oauth/yandex/login", nil))

	if recorder.Code != http.StatusFound {
		t.Fatalf("status %d, want 302", recorder.Code)
	}
	if got := recorder.Header().Get("Location"); got != "/" {
		t.Fatalf("Location = %q, want /", got)
	}

	sessionCookie := cookieNamed(recorder.Result().Cookies(), "rumcp_session")
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("no session cookie after login")
	}

	sealed, ok := f.repo.AccountTokenEnc(userID(t, f.repo), "yandex")
	if !ok {
		t.Fatal("account was not stored")
	}
	if sealed == testToken || strings.Contains(sealed, testToken) {
		t.Fatalf("stored token is plaintext: %q", sealed)
	}
	if !strings.HasPrefix(sealed, "v1:") {
		t.Fatalf("stored token is not in the sealed format: %q", sealed)
	}
	opened, err := f.box.Open(sealed)
	if err != nil {
		t.Fatalf("Open stored token: %v", err)
	}
	if opened != testToken {
		t.Fatalf("stored token opens to %q, want %q", opened, testToken)
	}

	if f.yandex.gotToken != "" {
		t.Fatalf("dev login called Yandex with %q, want a profile built from the env token", f.yandex.gotToken)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(sessionCookie)
	user, ok := f.handler.CurrentUser(req)
	if !ok {
		t.Fatal("CurrentUser did not resolve the session")
	}
	if user.Login != "dev" || user.Email != "dev@localhost" {
		t.Fatalf("user = %+v, want the dev profile", user)
	}
}

// TestDevLoginWithoutYandex replays the QA defect: no Yandex application and no
// live token, so the profile call would fail — the dev login still has to work.
func TestDevLoginWithoutYandex(t *testing.T) {
	f := newFixture(t, true)
	f.yandex.profileErr = errors.New("yandex is unreachable")

	recorder := httptest.NewRecorder()
	f.handler.Login(recorder, httptest.NewRequest(http.MethodGet, "/oauth/yandex/login", nil))
	if recorder.Code != http.StatusFound {
		t.Fatalf("status %d (%s), want 302", recorder.Code, recorder.Body.String())
	}
	if cookieNamed(recorder.Result().Cookies(), "rumcp_session") == nil {
		t.Fatal("no session cookie for a dev login")
	}
	if users := f.repo.Users(); len(users) != 1 {
		t.Fatalf("%d users after a dev login, want 1", len(users))
	}
	if f.yandex.gotToken != "" {
		t.Fatalf("dev login called Yandex with %q", f.yandex.gotToken)
	}

	// The same token is the same user: a second login updates, never duplicates.
	second := httptest.NewRecorder()
	f.handler.Login(second, httptest.NewRequest(http.MethodGet, "/oauth/yandex/login", nil))
	users := f.repo.Users()
	if len(users) != 1 {
		t.Fatalf("%d users after a second dev login, want 1", len(users))
	}
	if users[0].YandexID == "" || users[0].Login != "dev" {
		t.Fatalf("dev user = %+v", users[0])
	}
}

func TestLoginRedirectsToYandex(t *testing.T) {
	f := newFixture(t, false)
	recorder := httptest.NewRecorder()
	f.handler.Login(recorder, httptest.NewRequest(http.MethodGet, "/oauth/yandex/login", nil))

	if recorder.Code != http.StatusFound {
		t.Fatalf("status %d, want 302", recorder.Code)
	}
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if location.Host != "oauth.yandex.ru" {
		t.Fatalf("Location host = %q, want oauth.yandex.ru", location.Host)
	}
	query := location.Query()
	if query.Get("client_id") != "app-id" {
		t.Errorf("client_id = %q", query.Get("client_id"))
	}
	if query.Get("redirect_uri") != "http://localhost:8080"+auth.CallbackPath {
		t.Errorf("redirect_uri = %q", query.Get("redirect_uri"))
	}
	if query.Get("state") == "" {
		t.Error("no state in the authorize redirect")
	}
	if cookieNamed(recorder.Result().Cookies(), "rumcp_oauth_state") == nil {
		t.Error("state cookie was not set")
	}
}

func TestCallbackRejectsWrongState(t *testing.T) {
	f := newFixture(t, false)
	req := httptest.NewRequest(http.MethodGet, auth.CallbackPath+"?code=abc&state=wrong", nil)
	req.AddCookie(&http.Cookie{Name: "rumcp_oauth_state", Value: "expected"})
	recorder := httptest.NewRecorder()
	f.handler.Callback(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", recorder.Code)
	}
	if f.yandex.gotCode != "" {
		t.Fatal("code was exchanged despite the state mismatch")
	}
}

func TestCallbackExchangesCodeAndOpensSession(t *testing.T) {
	f := newFixture(t, false)
	req := httptest.NewRequest(http.MethodGet, auth.CallbackPath+"?code=the-code&state=the-state", nil)
	req.AddCookie(&http.Cookie{Name: "rumcp_oauth_state", Value: "the-state"})
	recorder := httptest.NewRecorder()
	f.handler.Callback(recorder, req)

	if recorder.Code != http.StatusFound {
		t.Fatalf("status %d, want 302", recorder.Code)
	}
	if f.yandex.gotCode != "the-code" {
		t.Fatalf("exchanged code %q", f.yandex.gotCode)
	}
	if f.yandex.gotRedirect != "http://localhost:8080"+auth.CallbackPath {
		t.Fatalf("redirect_uri = %q", f.yandex.gotRedirect)
	}
	if cookieNamed(recorder.Result().Cookies(), "rumcp_session") == nil {
		t.Fatal("no session cookie after the callback")
	}

	account, err := f.repo.AccountByUser(context.Background(), userID(t, f.repo), "yandex")
	if err != nil {
		t.Fatalf("AccountByUser: %v", err)
	}
	if account.Scope != "metrika:read" {
		t.Errorf("scope = %q", account.Scope)
	}
	if account.ExpiresAt == nil {
		t.Error("expires_at was not stored")
	}
	opened, err := f.box.Open(account.AccessTokenEnc)
	if err != nil || opened != testToken {
		t.Fatalf("access token opens to %q (%v)", opened, err)
	}
}

func TestCallbackHonoursReturnPath(t *testing.T) {
	f := newFixture(t, false)

	loginReq := httptest.NewRequest(http.MethodGet, "/oauth/yandex/login?return_to=%2Foauth%2Fauthorize%3Fclient_id%3Dx", nil)
	loginRecorder := httptest.NewRecorder()
	f.handler.Login(loginRecorder, loginReq)
	returnCookie := cookieNamed(loginRecorder.Result().Cookies(), "rumcp_return")
	if returnCookie == nil {
		t.Fatal("return path cookie was not set")
	}

	callbackReq := httptest.NewRequest(http.MethodGet, auth.CallbackPath+"?code=c&state=s", nil)
	callbackReq.AddCookie(&http.Cookie{Name: "rumcp_oauth_state", Value: "s"})
	callbackReq.AddCookie(returnCookie)
	recorder := httptest.NewRecorder()
	f.handler.Callback(recorder, callbackReq)

	if got := recorder.Header().Get("Location"); got != "/oauth/authorize?client_id=x" {
		t.Fatalf("Location = %q, want the authorization request", got)
	}
}

func TestCallbackRejectsForeignReturnPath(t *testing.T) {
	f := newFixture(t, false)

	loginReq := httptest.NewRequest(http.MethodGet, "/oauth/yandex/login?return_to=%2F%2Fevil.example", nil)
	loginRecorder := httptest.NewRecorder()
	f.handler.Login(loginRecorder, loginReq)
	if cookie := cookieNamed(loginRecorder.Result().Cookies(), "rumcp_return"); cookie != nil {
		t.Fatalf("return path cookie was set to %q", cookie.Value)
	}

	callbackReq := httptest.NewRequest(http.MethodGet, auth.CallbackPath+"?code=c&state=s", nil)
	callbackReq.AddCookie(&http.Cookie{Name: "rumcp_oauth_state", Value: "s"})
	recorder := httptest.NewRecorder()
	f.handler.Callback(recorder, callbackReq)

	if got := recorder.Header().Get("Location"); got != "/" {
		t.Fatalf("Location = %q, want / for a foreign return path", got)
	}
}

func TestReturnPathValidation(t *testing.T) {
	allowed := []string{"/", "/oauth/authorize?client_id=abc&state=1", "/api/me"}
	for _, path := range allowed {
		if !auth.ValidReturnPath(path) {
			t.Errorf("ValidReturnPath(%q) = false, want true", path)
		}
	}

	// "\" is read as "/" by browsers, so "/\evil.example" would leave the host.
	rejected := []string{
		"", "//evil.example", "/\\evil.example", "/\t/evil.example", "\\evil.example",
		"https://evil.example", "http://evil.example/path", "javascript:alert(1)",
		"evil.example/path", "/ok\r\nSet-Cookie: x=1", "//", "/\x00",
	}
	for _, path := range rejected {
		if auth.ValidReturnPath(path) {
			t.Errorf("ValidReturnPath(%q) = true, want false", path)
		}
	}
}

func TestSetReturnPathIgnoresForeignPath(t *testing.T) {
	f := newFixture(t, false)
	recorder := httptest.NewRecorder()
	f.session.SetReturnPath(recorder, "/\\evil.example")
	if cookie := cookieNamed(recorder.Result().Cookies(), "rumcp_return"); cookie != nil {
		t.Fatalf("return path cookie was set to %q", cookie.Value)
	}
}

func TestCallbackDropsPlantedReturnPath(t *testing.T) {
	f := newFixture(t, false)

	loginReq := httptest.NewRequest(http.MethodGet, "/oauth/yandex/login?return_to=%2F%5Cevil.example", nil)
	loginRecorder := httptest.NewRecorder()
	f.handler.Login(loginRecorder, loginReq)
	if cookie := cookieNamed(loginRecorder.Result().Cookies(), "rumcp_return"); cookie != nil {
		t.Fatalf("return path cookie was set to %q", cookie.Value)
	}

	callbackReq := httptest.NewRequest(http.MethodGet, auth.CallbackPath+"?code=c&state=s", nil)
	callbackReq.AddCookie(&http.Cookie{Name: "rumcp_oauth_state", Value: "s"})
	// Even a cookie planted by another site must not redirect off the host.
	callbackReq.AddCookie(&http.Cookie{Name: "rumcp_return", Value: "//evil.example"})
	recorder := httptest.NewRecorder()
	f.handler.Callback(recorder, callbackReq)

	if got := recorder.Header().Get("Location"); got != "/" {
		t.Fatalf("Location = %q, want /", got)
	}
}

func TestCurrentUserWithoutSession(t *testing.T) {
	f := newFixture(t, false)
	if _, ok := f.handler.CurrentUser(httptest.NewRequest(http.MethodGet, "/api/me", nil)); ok {
		t.Fatal("CurrentUser resolved a user without a session")
	}
}

func TestForgedSessionIsRejected(t *testing.T) {
	f := newFixture(t, false)
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: "rumcp_session", Value: "user-id|9999999999.forged-signature"})
	if _, ok := f.session.UserID(req); ok {
		t.Fatal("a forged session cookie was accepted")
	}
}

// userID returns the single user of the memory store.
func userID(t *testing.T, repo *storetest.Memory) string {
	t.Helper()
	users := repo.Users()
	if len(users) != 1 {
		t.Fatalf("store has %d users, want 1", len(users))
	}
	return users[0].ID
}
