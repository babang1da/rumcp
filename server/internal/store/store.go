// Package store keeps the service data in Postgres.
package store

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned when the requested row does not exist.
var ErrNotFound = errors.New("store: not found")

// ErrTokenConsumed is returned when a code or token was already used.
var ErrTokenConsumed = errors.New("store: token already consumed")

// User is a person who signed in through Yandex.
type User struct {
	ID        string
	YandexID  string
	Login     string
	Email     string
	Name      string
	CreatedAt time.Time
}

// Account is a connected Yandex account. Tokens are sealed by the caller.
type Account struct {
	UserID          string
	Provider        string
	AccessTokenEnc  string
	RefreshTokenEnc string
	Scope           string
	ExpiresAt       *time.Time
	ConnectedAt     time.Time
}

// OAuthClient is an MCP client registered through dynamic client registration.
type OAuthClient struct {
	ClientID                string
	ClientSecretHash        string
	ClientName              string
	RedirectURIs            []string
	GrantTypes              []string
	ResponseTypes           []string
	TokenEndpointAuthMethod string
	CreatedAt               time.Time
}

// Token kinds.
const (
	TokenKindCode    = "code"
	TokenKindAccess  = "access"
	TokenKindRefresh = "refresh"
)

// OAuthToken is an authorization code, access token or refresh token. Only the
// SHA-256 hash of the secret is stored.
type OAuthToken struct {
	ID                  string
	UserID              string
	ClientID            string
	Kind                string
	TokenHash           string
	CodeChallenge       string
	CodeChallengeMethod string
	RedirectURI         string
	Scope               string
	ExpiresAt           time.Time
	ConsumedAt          *time.Time
	CreatedAt           time.Time
}

// Lead is a request for a connector that is not released yet.
type Lead struct {
	ID        string
	Email     string
	Service   string
	UserID    string
	CreatedAt time.Time
}

// Usage is the call_log summary of one user.
type Usage struct {
	CallsToday  int
	ByConnector map[string]int
}

// Repository is the data access contract of the service.
type Repository interface {
	UpsertUser(ctx context.Context, u User) (User, error)
	UserByID(ctx context.Context, id string) (User, error)

	SaveAccount(ctx context.Context, a Account) error
	AccountByUser(ctx context.Context, userID, provider string) (Account, error)
	DeleteAccount(ctx context.Context, userID, provider string) error

	CreateClient(ctx context.Context, c OAuthClient) error
	ClientByID(ctx context.Context, clientID string) (OAuthClient, error)

	CreateToken(ctx context.Context, t OAuthToken) error
	TokenByHash(ctx context.Context, hash, kind string) (OAuthToken, error)
	ConsumeToken(ctx context.Context, id string) error

	RecordCall(ctx context.Context, userID, connector, tool, status string) error
	Usage(ctx context.Context, userID string, since time.Time) (Usage, error)

	CreateLead(ctx context.Context, l Lead) error
}
