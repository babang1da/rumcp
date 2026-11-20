package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/babang1da/rumcp/server/internal/store"
)

// ErrInvalidToken is returned when a Bearer token is unknown, expired or revoked.
var ErrInvalidToken = errors.New("oauth: invalid bearer token")

type userIDKey struct{}

// UserIDFromContext returns the user behind a validated Bearer token.
func UserIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(userIDKey{}).(string)
	return userID, ok
}

// VerifyBearer resolves the user of a Bearer token.
func (h *Handler) VerifyBearer(ctx context.Context, raw string) (string, error) {
	if raw == "" {
		return "", ErrInvalidToken
	}
	token, err := h.Repo.TokenByHash(ctx, hashSecret(raw), store.TokenKindAccess)
	if err != nil {
		return "", ErrInvalidToken
	}
	if token.ConsumedAt != nil || h.now().After(token.ExpiresAt) {
		return "", ErrInvalidToken
	}
	if _, err := h.Repo.UserByID(ctx, token.UserID); err != nil {
		return "", ErrInvalidToken
	}
	return token.UserID, nil
}

// RequireBearer rejects a request without a valid Bearer token and, as the MCP
// authorization spec requires, points the client at the resource metadata.
func (h *Handler) RequireBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := bearerToken(r)
		var userID string
		if ok {
			userID, _ = h.VerifyBearer(r.Context(), raw)
		}
		if userID == "" {
			h.challenge(w)
			writeOAuthError(w, http.StatusUnauthorized, "invalid_token", "a valid Bearer token is required")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userIDKey{}, userID)))
	})
}

func (h *Handler) challenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate",
		fmt.Sprintf(`Bearer realm="rumcp", resource_metadata=%q`, h.Cfg.Issuer+"/.well-known/oauth-protected-resource"))
}

func bearerToken(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false
	}
	return token, true
}
