// Package oauth implements OAuth 2.1 for MCP clients: discovery metadata,
// dynamic client registration, the authorization code grant with PKCE and
// Bearer validation for /mcp.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/babang1da/rumcp/server/internal/store"
)

// ScopeMCP is the only scope the service issues.
const ScopeMCP = "mcp"

// Repository is the data access the OAuth server needs.
type Repository interface {
	CreateClient(ctx context.Context, c store.OAuthClient) error
	ClientByID(ctx context.Context, clientID string) (store.OAuthClient, error)
	CreateToken(ctx context.Context, t store.OAuthToken) error
	TokenByHash(ctx context.Context, hash, kind string) (store.OAuthToken, error)
	ConsumeToken(ctx context.Context, id string) error
	UserByID(ctx context.Context, id string) (store.User, error)
}

// SessionReader tells which user is signed in to the cabinet.
type SessionReader interface {
	UserID(r *http.Request) (string, bool)
}

// Config holds the OAuth server settings.
type Config struct {
	// Issuer is the public origin, e.g. https://rumcp.ru.
	Issuer     string
	AccessTTL  time.Duration
	RefreshTTL time.Duration
	CodeTTL    time.Duration
	// CSRFKey signs the consent form. The service passes its session secret.
	CSRFKey []byte
}

// DefaultConfig returns the production timings.
func DefaultConfig(issuer string) Config {
	return Config{
		Issuer:     strings.TrimSuffix(issuer, "/"),
		AccessTTL:  time.Hour,
		RefreshTTL: 30 * 24 * time.Hour,
		CodeTTL:    time.Minute,
	}
}

// Handler serves the OAuth 2.1 endpoints of the service.
type Handler struct {
	Repo    Repository
	Session SessionReader
	Cfg     Config
	Now     func() time.Time
}

// Metadata serves /.well-known/oauth-authorization-server.
func (h *Handler) Metadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                h.Cfg.Issuer,
		"authorization_endpoint":                h.Cfg.Issuer + "/oauth/authorize",
		"token_endpoint":                        h.Cfg.Issuer + "/oauth/token",
		"registration_endpoint":                 h.Cfg.Issuer + "/oauth/register",
		"scopes_supported":                      []string{ScopeMCP},
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post", "client_secret_basic"},
	})
}

// ProtectedResource serves /.well-known/oauth-protected-resource.
func (h *Handler) ProtectedResource(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 h.Cfg.Issuer + "/mcp",
		"resource_name":            "RuMCP",
		"authorization_servers":    []string{h.Cfg.Issuer},
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         []string{ScopeMCP},
	})
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// newSecret returns a random URL-safe secret and its stored hash.
func newSecret() (secret, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	secret = base64.RawURLEncoding.EncodeToString(b)
	return secret, hashSecret(secret), nil
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	body := map[string]string{"error": code}
	if description != "" {
		body["error_description"] = description
	}
	writeJSON(w, status, body)
}

func redirectOAuthError(w http.ResponseWriter, r *http.Request, redirectURI, code, description, state string) {
	query := url.Values{"error": {code}}
	if description != "" {
		query.Set("error_description", description)
	}
	if state != "" {
		query.Set("state", state)
	}
	separator := "?"
	if strings.Contains(redirectURI, "?") {
		separator = "&"
	}
	http.Redirect(w, r, redirectURI+separator+query.Encode(), http.StatusFound)
}
