package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// YandexToken is the token response of the Yandex OAuth server.
type YandexToken struct {
	AccessToken  string
	RefreshToken string
	Scope        string
	ExpiresIn    int
}

// YandexProfile is the part of the Yandex user profile the service keeps.
type YandexProfile struct {
	ID    string
	Login string
	Email string
	Name  string
}

// YandexAPI is the Yandex OAuth surface used by the login flow.
type YandexAPI interface {
	Exchange(ctx context.Context, code, redirectURI string) (YandexToken, error)
	Profile(ctx context.Context, accessToken string) (YandexProfile, error)
}

// HTTPYandex talks to the real Yandex OAuth server.
type HTTPYandex struct {
	ClientID     string
	ClientSecret string
	TokenURL     string
	ProfileURL   string
	Client       *http.Client
}

// NewHTTPYandex builds a client for the production Yandex endpoints.
func NewHTTPYandex(clientID, clientSecret string) *HTTPYandex {
	return &HTTPYandex{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     "https://oauth.yandex.ru/token",
		ProfileURL:   "https://login.yandex.ru/info",
		Client:       &http.Client{Timeout: 10 * time.Second},
	}
}

// Exchange trades an authorization code for a Yandex token.
func (y *HTTPYandex) Exchange(ctx context.Context, code, redirectURI string) (YandexToken, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {y.ClientID},
		"client_secret": {y.ClientSecret},
		"redirect_uri":  {redirectURI},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, y.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return YandexToken{}, fmt.Errorf("yandex: exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := y.Client.Do(req)
	if err != nil {
		return YandexToken{}, fmt.Errorf("yandex: exchange: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return YandexToken{}, fmt.Errorf("yandex: exchange body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return YandexToken{}, fmt.Errorf("yandex: exchange status %d: %s", resp.StatusCode, body)
	}
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return YandexToken{}, fmt.Errorf("yandex: exchange decode: %w", err)
	}
	if payload.AccessToken == "" {
		return YandexToken{}, fmt.Errorf("yandex: exchange returned no access token")
	}
	return YandexToken{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		Scope:        payload.Scope,
		ExpiresIn:    payload.ExpiresIn,
	}, nil
}

// Profile loads the Yandex account behind an access token.
func (y *HTTPYandex) Profile(ctx context.Context, accessToken string) (YandexProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, y.ProfileURL+"?format=json", nil)
	if err != nil {
		return YandexProfile{}, fmt.Errorf("yandex: profile request: %w", err)
	}
	req.Header.Set("Authorization", "OAuth "+accessToken)

	resp, err := y.Client.Do(req)
	if err != nil {
		return YandexProfile{}, fmt.Errorf("yandex: profile: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return YandexProfile{}, fmt.Errorf("yandex: profile body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return YandexProfile{}, fmt.Errorf("yandex: profile status %d: %s", resp.StatusCode, body)
	}
	var payload struct {
		ID           *json.Number `json:"id"`
		Login        string       `json:"login"`
		DefaultEmail string       `json:"default_email"`
		Emails       []string     `json:"emails"`
		RealName     string       `json:"real_name"`
		DisplayName  string       `json:"display_name"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return YandexProfile{}, fmt.Errorf("yandex: profile decode: %w", err)
	}
	if payload.ID == nil || payload.ID.String() == "" {
		return YandexProfile{}, fmt.Errorf("yandex: profile has no id")
	}

	email := payload.DefaultEmail
	if email == "" && len(payload.Emails) > 0 {
		email = payload.Emails[0]
	}
	name := payload.RealName
	if name == "" {
		name = payload.DisplayName
	}
	return YandexProfile{ID: payload.ID.String(), Login: payload.Login, Email: email, Name: name}, nil
}
