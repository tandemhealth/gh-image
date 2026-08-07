package cookies

import "net/http"

// NewSessionCookie builds a github.com user_session cookie from a raw value.
// Shape only — it does not trim or validate the value; callers handling
// user-supplied tokens layer those checks on top.
func NewSessionCookie(value string) *http.Cookie {
	return &http.Cookie{
		Name:     "user_session",
		Value:    value,
		Domain:   "github.com",
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
	}
}
