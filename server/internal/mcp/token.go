package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/babang1da/rumcp/server/internal/store"
)

// Errors that decide the message the user gets from a tool call.
var (
	// ErrNoAccount means the user has no connected Yandex account.
	ErrNoAccount = errors.New("mcp: yandex account is not connected")
	// ErrReconnect means the stored Yandex token is expired and cannot be
	// refreshed, so the user has to connect the account again.
	ErrReconnect = errors.New("mcp: yandex token cannot be refreshed")
)

// tokenSkew refreshes a token a minute before it actually expires, so a call
// never starts with a token that dies in flight.
const tokenSkew = time.Minute

// yandexTokenURL is the Yandex OAuth token endpoint.
const yandexTokenURL = "https://oauth.yandex.ru/token"

// RefreshedToken is the result of a Yandex token refresh.
type RefreshedToken struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
}

// TokenRefresher exchanges a Yandex refresh token for a new access token.
type TokenRefresher interface {
	Refresh(ctx context.Context, refreshToken string) (RefreshedToken, error)
}

// YandexRefresher refreshes tokens at the real Yandex OAuth server.
type YandexRefresher struct {
	ClientID     string
	ClientSecret string
	// TokenURL is overridable for tests.
	TokenURL string
	Client   *http.Client
}

// NewYandexRefresher builds the production refresher.
func NewYandexRefresher(clientID, clientSecret string) *YandexRefresher {
	return &YandexRefresher{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     yandexTokenURL,
		Client:       &http.Client{Timeout: 10 * time.Second},
	}
}

// Refresh implements TokenRefresher.
func (y *YandexRefresher) Refresh(ctx context.Context, refreshToken string) (RefreshedToken, error) {
	endpoint := y.TokenURL
	if endpoint == "" {
		endpoint = yandexTokenURL
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {y.ClientID},
		"client_secret": {y.ClientSecret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return RefreshedToken{}, fmt.Errorf("yandex: refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := y.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return RefreshedToken{}, fmt.Errorf("yandex: refresh: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return RefreshedToken{}, fmt.Errorf("yandex: refresh body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return RefreshedToken{}, fmt.Errorf("yandex: refresh status %d: %s", resp.StatusCode, body)
	}

	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return RefreshedToken{}, fmt.Errorf("yandex: refresh decode: %w", err)
	}
	if payload.AccessToken == "" {
		return RefreshedToken{}, errors.New("yandex: refresh returned no access token")
	}
	return RefreshedToken{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		ExpiresIn:    payload.ExpiresIn,
	}, nil
}

// accessToken returns a valid Yandex access token of the user, refreshing it
// when it expired. The tokens live in the database sealed.
func (b *Bridge) accessToken(ctx context.Context, userID string) (string, error) {
	account, err := b.deps.Repo.AccountByUser(ctx, userID, providerYandex)
	if errors.Is(err, store.ErrNotFound) {
		return "", ErrNoAccount
	}
	if err != nil {
		return "", fmt.Errorf("mcp: account: %w", err)
	}
	if account.AccessTokenEnc == "" {
		return "", ErrNoAccount
	}

	accessToken, err := b.deps.Box.Open(account.AccessTokenEnc)
	if err != nil {
		return "", fmt.Errorf("mcp: open access token: %w", err)
	}
	if account.ExpiresAt == nil || b.now().Add(tokenSkew).Before(*account.ExpiresAt) {
		return accessToken, nil
	}

	if account.RefreshTokenEnc == "" || b.deps.Yandex == nil {
		return "", ErrReconnect
	}
	refreshToken, err := b.deps.Box.Open(account.RefreshTokenEnc)
	if err != nil || refreshToken == "" {
		return "", ErrReconnect
	}

	refreshed, err := b.deps.Yandex.Refresh(ctx, refreshToken)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrReconnect, err)
	}
	if err := b.saveTokens(ctx, account, refreshed); err != nil {
		// The new token works even if it is not stored yet; the next call
		// refreshes again. Losing it is better than failing the call.
		b.logger().Error("mcp: save refreshed token", "error", err, "user", userID)
	}
	return refreshed.AccessToken, nil
}

// saveTokens stores the refreshed tokens of the user.
func (b *Bridge) saveTokens(ctx context.Context, account store.Account, refreshed RefreshedToken) error {
	accessTokenEnc, err := b.deps.Box.Seal(refreshed.AccessToken)
	if err != nil {
		return err
	}
	refreshTokenEnc := account.RefreshTokenEnc
	if refreshed.RefreshToken != "" {
		if refreshTokenEnc, err = b.deps.Box.Seal(refreshed.RefreshToken); err != nil {
			return err
		}
	}
	var expiresAt *time.Time
	if refreshed.ExpiresIn > 0 {
		expires := b.now().Add(time.Duration(refreshed.ExpiresIn) * time.Second)
		expiresAt = &expires
	}
	return b.deps.Repo.SaveAccount(ctx, store.Account{
		UserID:          account.UserID,
		Provider:        account.Provider,
		AccessTokenEnc:  accessTokenEnc,
		RefreshTokenEnc: refreshTokenEnc,
		Scope:           account.Scope,
		ExpiresAt:       expiresAt,
	})
}
