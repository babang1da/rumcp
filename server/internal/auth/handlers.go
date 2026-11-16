package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/babang1da/rumcp/server/internal/secret"
	"github.com/babang1da/rumcp/server/internal/store"
)

// Repository is the data access the login flow needs.
type Repository interface {
	UpsertUser(ctx context.Context, u store.User) (store.User, error)
	UserByID(ctx context.Context, id string) (store.User, error)
	SaveAccount(ctx context.Context, a store.Account) error
}

// Config is the Yandex login configuration.
type Config struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
	TestToken    string
	// DevLogin enables signing in with TestToken while no Yandex application exists.
	DevLogin bool
}

// Handler serves the Yandex login and turns it into a cabinet session.
type Handler struct {
	Repo    Repository
	Yandex  YandexAPI
	Session *Session
	Box     *secret.Box
	Cfg     Config
	Now     func() time.Time
}

// CallbackPath is where Yandex redirects after the user grants access.
const CallbackPath = "/oauth/yandex/callback"

// Login starts the Yandex login, or signs in with the dev token.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if returnTo := r.URL.Query().Get("return_to"); returnTo != "" {
		h.Session.SetReturnPath(w, returnTo)
	}

	if h.Cfg.DevLogin {
		h.signIn(w, r, h.Cfg.TestToken, "", 0, "metrika:read webmaster:read")
		return
	}

	state, err := randomState()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "login_unavailable")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookie,
		Value:    state,
		Path:     "/",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   h.Session.Secure,
		SameSite: http.SameSiteLaxMode,
	})

	query := url.Values{
		"response_type": {"code"},
		"client_id":     {h.Cfg.ClientID},
		"redirect_uri":  {h.Cfg.BaseURL + CallbackPath},
		"state":         {state},
	}
	http.Redirect(w, r, "https://oauth.yandex.ru/authorize?"+query.Encode(), http.StatusFound)
}

// Callback finishes the Yandex login.
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if query.Get("error") != "" {
		http.Redirect(w, r, "/?login_error="+url.QueryEscape(query.Get("error")), http.StatusFound)
		return
	}

	cookie, err := r.Cookie(stateCookie)
	if err != nil || cookie.Value == "" || cookie.Value != query.Get("state") {
		writeError(w, http.StatusBadRequest, "invalid_state")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: h.Session.Secure, SameSite: http.SameSiteLaxMode})

	code := query.Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "code_missing")
		return
	}

	token, err := h.Yandex.Exchange(r.Context(), code, h.Cfg.BaseURL+CallbackPath)
	if err != nil {
		writeError(w, http.StatusBadGateway, "yandex_exchange_failed")
		return
	}
	h.signIn(w, r, token.AccessToken, token.RefreshToken, token.ExpiresIn, token.Scope)
}

// CurrentUser resolves the user of the session cookie.
func (h *Handler) CurrentUser(r *http.Request) (store.User, bool) {
	userID, ok := h.Session.UserID(r)
	if !ok {
		return store.User{}, false
	}
	user, err := h.Repo.UserByID(r.Context(), userID)
	if err != nil {
		return store.User{}, false
	}
	return user, true
}

// signIn stores the Yandex token sealed, opens a session and redirects.
func (h *Handler) signIn(w http.ResponseWriter, r *http.Request, accessToken, refreshToken string, expiresIn int, scope string) {
	ctx := r.Context()

	profile, err := h.Yandex.Profile(ctx, accessToken)
	if err != nil {
		writeError(w, http.StatusBadGateway, "yandex_profile_failed")
		return
	}

	user, err := h.Repo.UpsertUser(ctx, store.User{
		YandexID: profile.ID,
		Login:    profile.Login,
		Email:    profile.Email,
		Name:     profile.Name,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "user_save_failed")
		return
	}

	account := store.Account{
		UserID:   user.ID,
		Provider: "yandex",
		Scope:    scope,
	}
	if account.AccessTokenEnc, err = h.Box.Seal(accessToken); err != nil {
		writeError(w, http.StatusInternalServerError, "token_seal_failed")
		return
	}
	if refreshToken != "" {
		if account.RefreshTokenEnc, err = h.Box.Seal(refreshToken); err != nil {
			writeError(w, http.StatusInternalServerError, "token_seal_failed")
			return
		}
	}
	if expiresIn > 0 {
		expires := h.now().Add(time.Duration(expiresIn) * time.Second)
		account.ExpiresAt = &expires
	}
	if err := h.Repo.SaveAccount(ctx, account); err != nil {
		writeError(w, http.StatusInternalServerError, "account_save_failed")
		return
	}

	h.Session.Set(w, user.ID)
	http.Redirect(w, r, h.Session.TakeReturnPath(w, r), http.StatusFound)
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func writeError(w http.ResponseWriter, status int, code string) {
	body, err := json.Marshal(map[string]string{"error": code})
	if err != nil {
		body = []byte(`{"error":"internal"}`)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
