package oauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"html/template"
	"log/slog"
	"net/http"
)

// consentPage asks the user to let an MCP client read their Yandex data. The
// form posts back to /oauth/authorize with a token bound to the session user and
// the client, so another site cannot approve a client on the user's behalf.
var consentPage = template.Must(template.New("consent").Parse(`<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>Доступ к Метрике и Вебмастеру — RuMCP</title>
<style>
body { font-family: system-ui, -apple-system, "Segoe UI", sans-serif; margin: 0; background: #f6f7f9; color: #1a1a1a; }
main { max-width: 30rem; margin: 4rem auto; padding: 2rem; background: #fff; border-radius: 12px; box-shadow: 0 1px 3px rgba(0,0,0,.08); }
h1 { font-size: 1.25rem; margin: 0 0 1rem; }
p { line-height: 1.5; }
.client { font-weight: 600; }
ul { padding-left: 1.2rem; line-height: 1.6; }
.actions { display: flex; gap: .75rem; margin-top: 1.5rem; }
button { flex: 1; padding: .75rem 1rem; font-size: 1rem; border-radius: 8px; border: 1px solid transparent; cursor: pointer; }
.allow { background: #fc3f1d; color: #fff; }
.deny { background: #fff; border-color: #ccc; color: #1a1a1a; }
.note { font-size: .875rem; color: #666; margin-top: 1.5rem; }
</style>
</head>
<body>
<main>
<h1>Разрешить доступ?</h1>
<p>Клиент <span class="client">{{if .ClientName}}{{.ClientName}}{{else}}без имени{{end}}</span>
хочет читать данные вашего аккаунта Яндекса через RuMCP:</p>
<ul>
<li>Яндекс Метрика — счётчики и отчёты;</li>
<li>Яндекс Вебмастер — сайты, индексация, поисковые запросы.</li>
</ul>
<p class="note">Доступ только на чтение. Отозвать его можно в кабинете RuMCP, отключив аккаунт Яндекса.</p>
<form method="post" action="/oauth/authorize">
<input type="hidden" name="client_id" value="{{.ClientID}}">
<input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
<input type="hidden" name="response_type" value="code">
<input type="hidden" name="state" value="{{.State}}">
<input type="hidden" name="scope" value="{{.Scope}}">
<input type="hidden" name="code_challenge" value="{{.Challenge}}">
<input type="hidden" name="code_challenge_method" value="S256">
<input type="hidden" name="csrf" value="{{.CSRF}}">
<div class="actions">
<button class="allow" type="submit" name="decision" value="allow">Разрешить</button>
<button class="deny" type="submit" name="decision" value="deny">Отклонить</button>
</div>
</form>
</main>
</body>
</html>
`))

type consentData struct {
	ClientName  string
	ClientID    string
	RedirectURI string
	State       string
	Scope       string
	Challenge   string
	CSRF        string
}

// showConsent renders the consent screen of a validated request.
func (h *Handler) showConsent(w http.ResponseWriter, userID string, req authorizeRequest) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	// The consent screen must not be clickjackable: one framed click would grant
	// a client access to the Yandex account of the user.
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'")

	data := consentData{
		ClientName:  req.Client.ClientName,
		ClientID:    req.Client.ClientID,
		RedirectURI: req.RedirectURI,
		State:       req.State,
		Scope:       req.Scope,
		Challenge:   req.Challenge,
		CSRF:        h.csrfToken(userID, req),
	}
	if err := consentPage.Execute(w, data); err != nil {
		slog.Error("consent page", "error", err)
	}
}

// csrfToken binds the consent form to the user and the exact request, so a token
// shown for one redirect_uri or challenge cannot approve another.
func (h *Handler) csrfToken(userID string, req authorizeRequest) string {
	mac := hmac.New(sha256.New, h.Cfg.CSRFKey)
	for _, part := range []string{"consent", userID, req.Client.ClientID, req.RedirectURI,
		req.Challenge, req.State, req.Scope} {
		mac.Write([]byte(part))
		mac.Write([]byte{0})
	}
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// validCSRF compares a submitted consent token in constant time.
func (h *Handler) validCSRF(userID string, req authorizeRequest, token string) bool {
	if token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(h.csrfToken(userID, req)), []byte(token)) == 1
}
