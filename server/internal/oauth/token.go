package oauth

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/babang1da/rumcp/server/internal/store"
)

// Token implements the token endpoint: authorization_code and refresh_token.
func (h *Handler) Token(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeOAuthError(w, http.StatusMethodNotAllowed, "invalid_request", "use POST")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "body is not a form")
		return
	}

	client, ok := h.authenticateClient(w, r)
	if !ok {
		return
	}

	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		h.tokenFromCode(w, r, client)
	case "refresh_token":
		h.tokenFromRefresh(w, r, client)
	case "":
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "grant_type is required")
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "only authorization_code and refresh_token are supported")
	}
}

// authenticateClient resolves the client and checks its secret when it has one.
func (h *Handler) authenticateClient(w http.ResponseWriter, r *http.Request) (store.OAuthClient, bool) {
	clientID := r.PostForm.Get("client_id")
	secret := r.PostForm.Get("client_secret")

	if clientID == "" {
		basicID, basicSecret, ok := r.BasicAuth()
		if !ok {
			writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "client_id is required")
			return store.OAuthClient{}, false
		}
		clientID, secret = basicID, basicSecret
	}
	// RFC 6749: a confidential client sending its secret in the body must not
	// also use the Authorization header.
	if _, _, ok := r.BasicAuth(); ok && r.PostForm.Get("client_secret") != "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "use one client authentication method")
		return store.OAuthClient{}, false
	}

	client, err := h.Repo.ClientByID(r.Context(), clientID)
	if errors.Is(err, store.ErrNotFound) {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "unknown client")
		return store.OAuthClient{}, false
	}
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "cannot load client")
		return store.OAuthClient{}, false
	}
	if client.ClientSecretHash != "" &&
		subtle.ConstantTimeCompare([]byte(hashSecret(secret)), []byte(client.ClientSecretHash)) != 1 {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "client authentication failed")
		return store.OAuthClient{}, false
	}
	return client, true
}

func (h *Handler) tokenFromCode(w http.ResponseWriter, r *http.Request, client store.OAuthClient) {
	code := r.PostForm.Get("code")
	if code == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "code is required")
		return
	}

	token, err := h.Repo.TokenByHash(r.Context(), hashSecret(code), store.TokenKindCode)
	if errors.Is(err, store.ErrNotFound) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "unknown authorization code")
		return
	}
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "cannot load authorization code")
		return
	}
	if token.ClientID != client.ClientID {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code belongs to another client")
		return
	}
	if token.ConsumedAt != nil || h.now().After(token.ExpiresAt) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code is used or expired")
		return
	}
	if token.RedirectURI != "" && r.PostForm.Get("redirect_uri") != token.RedirectURI {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri does not match the authorization request")
		return
	}

	verifier := r.PostForm.Get("code_verifier")
	if verifier == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "code_verifier is required")
		return
	}
	if !verifyPKCE(token.CodeChallenge, verifier) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "code_verifier does not match code_challenge")
		return
	}

	if err := h.Repo.ConsumeToken(r.Context(), token.ID); err != nil {
		h.writeConsumeError(w, err, "authorization code")
		return
	}
	h.issueTokens(w, r, client, token.UserID, token.Scope)
}

func (h *Handler) tokenFromRefresh(w http.ResponseWriter, r *http.Request, client store.OAuthClient) {
	raw := r.PostForm.Get("refresh_token")
	if raw == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "refresh_token is required")
		return
	}

	token, err := h.Repo.TokenByHash(r.Context(), hashSecret(raw), store.TokenKindRefresh)
	if errors.Is(err, store.ErrNotFound) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "unknown refresh token")
		return
	}
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "cannot load refresh token")
		return
	}
	if token.ClientID != client.ClientID {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh token belongs to another client")
		return
	}
	if token.ConsumedAt != nil || h.now().After(token.ExpiresAt) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh token is used or expired")
		return
	}

	if err := h.Repo.ConsumeToken(r.Context(), token.ID); err != nil {
		h.writeConsumeError(w, err, "refresh token")
		return
	}
	h.issueTokens(w, r, client, token.UserID, token.Scope)
}

// issueTokens creates an access token (and a refresh token when the client may
// use one) and responds with the token payload.
func (h *Handler) issueTokens(w http.ResponseWriter, r *http.Request, client store.OAuthClient, userID, scope string) {
	access, accessHash, err := newSecret()
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "cannot issue access token")
		return
	}
	if scope == "" {
		scope = ScopeMCP
	}
	err = h.Repo.CreateToken(r.Context(), store.OAuthToken{
		UserID:    userID,
		ClientID:  client.ClientID,
		Kind:      store.TokenKindAccess,
		TokenHash: accessHash,
		Scope:     scope,
		ExpiresAt: h.now().Add(h.Cfg.AccessTTL),
	})
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "cannot issue access token")
		return
	}

	response := map[string]any{
		"access_token": access,
		"token_type":   "Bearer",
		"expires_in":   int(h.Cfg.AccessTTL.Seconds()),
		"scope":        scope,
	}

	if includes(client.GrantTypes, "refresh_token") {
		refresh, refreshHash, err := newSecret()
		if err != nil {
			writeOAuthError(w, http.StatusInternalServerError, "server_error", "cannot issue refresh token")
			return
		}
		err = h.Repo.CreateToken(r.Context(), store.OAuthToken{
			UserID:    userID,
			ClientID:  client.ClientID,
			Kind:      store.TokenKindRefresh,
			TokenHash: refreshHash,
			Scope:     scope,
			ExpiresAt: h.now().Add(h.Cfg.RefreshTTL),
		})
		if err != nil {
			writeOAuthError(w, http.StatusInternalServerError, "server_error", "cannot issue refresh token")
			return
		}
		response["refresh_token"] = refresh
	}

	writeJSON(w, http.StatusOK, response)
}

// writeConsumeError reports a token that another request consumed first as an
// invalid grant, everything else as a server error.
func (h *Handler) writeConsumeError(w http.ResponseWriter, err error, name string) {
	if errors.Is(err, store.ErrTokenConsumed) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", name+" is used or expired")
		return
	}
	writeOAuthError(w, http.StatusInternalServerError, "server_error", "cannot consume "+name)
}

func includes(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return true
		}
	}
	return false
}
