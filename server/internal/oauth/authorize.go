package oauth

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/babang1da/rumcp/server/internal/store"
)

// authorizeRequest is a validated authorization request awaiting the decision
// of the user.
type authorizeRequest struct {
	Client      store.OAuthClient
	RedirectURI string
	State       string
	Scope       string
	Challenge   string
}

// Authorize serves the authorization endpoint: GET shows the consent screen,
// POST applies the user's decision and issues the code. MCP clients register
// themselves anonymously, so the user must confirm every client before it can
// read their Yandex data.
func (h *Handler) Authorize(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.authorizeRequest(w, r)
	case http.MethodPost:
		h.authorizeDecision(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeOAuthError(w, http.StatusMethodNotAllowed, "invalid_request", "use GET or POST")
	}
}

// authorizeRequest validates the request and shows the consent screen.
func (h *Handler) authorizeRequest(w http.ResponseWriter, r *http.Request) {
	req, ok := h.parseAuthorize(w, r, r.URL.Query())
	if !ok {
		return
	}

	userID, signedIn := h.Session.UserID(r)
	if !signedIn {
		http.Redirect(w, r, "/oauth/yandex/login?return_to="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
		return
	}
	h.showConsent(w, userID, req)
}

// authorizeDecision applies the answer of the consent form.
func (h *Handler) authorizeDecision(w http.ResponseWriter, r *http.Request) {
	userID, signedIn := h.Session.UserID(r)
	if !signedIn {
		writeOAuthError(w, http.StatusUnauthorized, "access_denied", "sign in to the cabinet first")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "body is not a form")
		return
	}

	req, ok := h.parseAuthorize(w, r, r.PostForm)
	if !ok {
		return
	}
	if !h.validCSRF(userID, req, r.PostForm.Get("csrf")) {
		writeOAuthError(w, http.StatusForbidden, "invalid_request", "consent token is missing or wrong")
		return
	}

	switch r.PostForm.Get("decision") {
	case "deny":
		redirectOAuthError(w, r, req.RedirectURI, "access_denied", "the user declined access", req.State)
		return
	case "allow":
	default:
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "decision must be allow or deny")
		return
	}

	h.issueCode(w, r, userID, req)
}

// issueCode stores a one-time authorization code and answers the client.
func (h *Handler) issueCode(w http.ResponseWriter, r *http.Request, userID string, req authorizeRequest) {
	code, codeHash, err := newSecret()
	if err != nil {
		redirectOAuthError(w, r, req.RedirectURI, "server_error", "cannot issue authorization code", req.State)
		return
	}
	err = h.Repo.CreateToken(r.Context(), store.OAuthToken{
		UserID:              userID,
		ClientID:            req.Client.ClientID,
		Kind:                store.TokenKindCode,
		TokenHash:           codeHash,
		CodeChallenge:       req.Challenge,
		CodeChallengeMethod: "S256",
		RedirectURI:         req.RedirectURI,
		Scope:               req.Scope,
		ExpiresAt:           h.now().Add(h.Cfg.CodeTTL),
	})
	if err != nil {
		redirectOAuthError(w, r, req.RedirectURI, "server_error", "cannot issue authorization code", req.State)
		return
	}

	params := url.Values{"code": {code}}
	if req.State != "" {
		params.Set("state", req.State)
	}
	separator := "?"
	if strings.Contains(req.RedirectURI, "?") {
		separator = "&"
	}
	http.Redirect(w, r, req.RedirectURI+separator+params.Encode(), http.StatusFound)
}

// parseAuthorize validates an authorization request. It writes the error itself
// and returns false when the request cannot be trusted.
func (h *Handler) parseAuthorize(w http.ResponseWriter, r *http.Request, params url.Values) (authorizeRequest, bool) {
	client, err := h.Repo.ClientByID(r.Context(), params.Get("client_id"))
	if errors.Is(err, store.ErrNotFound) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client", "unknown client_id")
		return authorizeRequest{}, false
	}
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "cannot load client")
		return authorizeRequest{}, false
	}

	redirectURI := params.Get("redirect_uri")
	if !contains(client.RedirectURIs, redirectURI) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "redirect_uri is not registered for this client")
		return authorizeRequest{}, false
	}
	state := params.Get("state")

	if !contains(client.GrantTypes, "authorization_code") {
		redirectOAuthError(w, r, redirectURI, "unauthorized_client", "client may not use the authorization code grant", state)
		return authorizeRequest{}, false
	}
	if params.Get("response_type") != "code" {
		redirectOAuthError(w, r, redirectURI, "unsupported_response_type", "only response_type=code is supported", state)
		return authorizeRequest{}, false
	}

	scope, err := requestedScope(params.Get("scope"))
	if err != nil {
		redirectOAuthError(w, r, redirectURI, "invalid_scope", err.Error(), state)
		return authorizeRequest{}, false
	}

	challenge := params.Get("code_challenge")
	if challenge == "" {
		redirectOAuthError(w, r, redirectURI, "invalid_request", "code_challenge is required", state)
		return authorizeRequest{}, false
	}
	if method := params.Get("code_challenge_method"); method != "S256" {
		redirectOAuthError(w, r, redirectURI, "invalid_request", "code_challenge_method must be S256", state)
		return authorizeRequest{}, false
	}
	if !validChallenge(challenge) {
		redirectOAuthError(w, r, redirectURI, "invalid_request", "malformed code_challenge", state)
		return authorizeRequest{}, false
	}

	return authorizeRequest{
		Client:      client,
		RedirectURI: redirectURI,
		State:       state,
		Scope:       scope,
		Challenge:   challenge,
	}, true
}

func contains(values []string, want string) bool {
	if want == "" {
		return false
	}
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// requestedScope validates the scope of an authorization request.
func requestedScope(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return ScopeMCP, nil
	}
	for _, scope := range strings.Fields(raw) {
		if scope != ScopeMCP {
			return "", errors.New("unknown scope " + scope)
		}
	}
	return ScopeMCP, nil
}
