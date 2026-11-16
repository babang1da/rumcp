package oauth

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/babang1da/rumcp/server/internal/store"
)

type registrationRequest struct {
	RedirectURIs            []string `json:"redirect_uris"`
	ClientName              string   `json:"client_name"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	Scope                   string   `json:"scope"`
}

// Register implements dynamic client registration (RFC 7591).
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeOAuthError(w, http.StatusMethodNotAllowed, "invalid_request", "use POST")
		return
	}

	var req registrationRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "body is not valid JSON")
		return
	}
	if len(req.RedirectURIs) == 0 {
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uris is required")
		return
	}
	for _, uri := range req.RedirectURIs {
		if !validRedirectURI(uri) {
			writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uri must be an absolute http(s) URL without a fragment")
			return
		}
	}

	authMethod := req.TokenEndpointAuthMethod
	if authMethod == "" {
		authMethod = "none"
	}
	if authMethod != "none" && authMethod != "client_secret_post" && authMethod != "client_secret_basic" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "unsupported token_endpoint_auth_method")
		return
	}

	grantTypes := req.GrantTypes
	if len(grantTypes) == 0 {
		grantTypes = []string{"authorization_code", "refresh_token"}
	}
	for _, grant := range grantTypes {
		if grant != "authorization_code" && grant != "refresh_token" {
			writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "unsupported grant_type")
			return
		}
	}
	responseTypes := req.ResponseTypes
	if len(responseTypes) == 0 {
		responseTypes = []string{"code"}
	}
	for _, responseType := range responseTypes {
		if responseType != "code" {
			writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "unsupported response_type")
			return
		}
	}

	clientID, _, err := newSecret()
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "cannot generate client id")
		return
	}
	clientID = "rumcp_" + clientID
	client := store.OAuthClient{
		ClientID:                clientID,
		ClientName:              req.ClientName,
		RedirectURIs:            req.RedirectURIs,
		GrantTypes:              grantTypes,
		ResponseTypes:           responseTypes,
		TokenEndpointAuthMethod: authMethod,
	}

	response := map[string]any{
		"client_id":                  client.ClientID,
		"client_id_issued_at":        h.now().Unix(),
		"client_name":                client.ClientName,
		"redirect_uris":              client.RedirectURIs,
		"grant_types":                client.GrantTypes,
		"response_types":             client.ResponseTypes,
		"token_endpoint_auth_method": client.TokenEndpointAuthMethod,
		"scope":                      ScopeMCP,
	}

	if authMethod != "none" {
		clientSecret, hash, err := newSecret()
		if err != nil {
			writeOAuthError(w, http.StatusInternalServerError, "server_error", "cannot generate client secret")
			return
		}
		client.ClientSecretHash = hash
		response["client_secret"] = clientSecret
		response["client_secret_expires_at"] = 0
	}

	if err := h.Repo.CreateClient(r.Context(), client); err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "cannot register client")
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func validRedirectURI(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Fragment != "" || parsed.Host == "" || parsed.User != nil {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	return true
}
