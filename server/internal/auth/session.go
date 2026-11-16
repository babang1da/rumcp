package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	sessionCookie = "rumcp_session"
	returnCookie  = "rumcp_return"
	stateCookie   = "rumcp_oauth_state"
)

// Session signs and verifies the cabinet cookie.
type Session struct {
	Secret []byte
	Secure bool
	TTL    time.Duration
}

// NewSession builds a session cookie signer.
func NewSession(secret []byte, secure bool) *Session {
	return &Session{Secret: secret, Secure: secure, TTL: 30 * 24 * time.Hour}
}

// Set writes the signed session cookie of a user.
func (s *Session) Set(w http.ResponseWriter, userID string) {
	expires := time.Now().Add(s.TTL)
	payload := userID + "|" + strconv.FormatInt(expires.Unix(), 10)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    payload + "." + s.sign(payload),
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(s.TTL.Seconds()),
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// Clear removes the session cookie.
func (s *Session) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// UserID returns the user id stored in a valid session cookie.
func (s *Session) UserID(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", false
	}
	payload, sig, ok := strings.Cut(cookie.Value, ".")
	if !ok || !hmac.Equal([]byte(sig), []byte(s.sign(payload))) {
		return "", false
	}
	userID, expiresRaw, ok := strings.Cut(payload, "|")
	if !ok || userID == "" {
		return "", false
	}
	expires, err := strconv.ParseInt(expiresRaw, 10, 64)
	if err != nil || time.Now().After(time.Unix(expires, 0)) {
		return "", false
	}
	return userID, true
}

func (s *Session) sign(payload string) string {
	mac := hmac.New(sha256.New, s.Secret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// SetReturnPath remembers where to continue after the Yandex login. A path that
// is not a same-origin location is dropped, so a crafted link cannot turn the
// login into an open redirect.
func (s *Session) SetReturnPath(w http.ResponseWriter, path string) {
	if !ValidReturnPath(path) {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     returnCookie,
		Value:    path,
		Path:     "/",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// TakeReturnPath returns the remembered path and clears the cookie.
func (s *Session) TakeReturnPath(w http.ResponseWriter, r *http.Request) string {
	cookie, err := r.Cookie(returnCookie)
	if err != nil {
		return "/"
	}
	http.SetCookie(w, &http.Cookie{Name: returnCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: s.Secure, SameSite: http.SameSiteLaxMode})
	if !ValidReturnPath(cookie.Value) {
		return "/"
	}
	return cookie.Value
}

// ValidReturnPath reports whether a value is a safe same-origin path to continue
// with after login: it must start with a single "/", carry no scheme or host, and
// contain no backslashes or control characters. Browsers read "\" as "/", so
// "/\evil.example" would otherwise become a redirect to another host.
func ValidReturnPath(raw string) bool {
	if raw == "" || raw[0] != '/' || strings.HasPrefix(raw, "//") {
		return false
	}
	for _, r := range raw {
		if r == '\\' || r < 0x20 || r == 0x7f {
			return false
		}
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if parsed.Scheme != "" || parsed.Host != "" || parsed.Opaque != "" {
		return false
	}
	return strings.HasPrefix(parsed.Path, "/") && !strings.HasPrefix(parsed.Path, "//")
}
