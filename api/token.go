package api

import (
	"net/http"
	"regexp"
	"strings"
	"time"
)

// TokenExpiresHeader carries the token's expiry as an RFC 3339 time. The
// platform sets it on responses to a token that expires within 14 days.
const TokenExpiresHeader = "Valar-Token-Expires"

// OnTokenExpiry, when set, is called for every response that carries
// TokenExpiresHeader.
var OnTokenExpiry func(endpoint string, expires time.Time)

// TokenRefusedError is a 401 whose challenge says the token expired or was
// revoked, such as "token 3f2a... expired at 2027-02-18T10:00:00Z".
type TokenRefusedError struct {
	Reason   string
	Endpoint string
}

func (err TokenRefusedError) Error() string { return err.Reason }

var challengeDescription = regexp.MustCompile(`error_description="([^"]*)"`)

func (client *Client) noteTokenExpiry(header http.Header) {
	value := header.Get(TokenExpiresHeader)
	if value == "" || OnTokenExpiry == nil {
		return
	}
	if expires, err := time.Parse(time.RFC3339, value); err == nil {
		OnTokenExpiry(client.Endpoint, expires)
	}
}

// responseError turns a non-2xx response into an Error.
func (client *Client) responseError(resp *http.Response, body []byte) error {
	serverErr := parseErrorResponse(body)
	if resp.StatusCode == http.StatusUnauthorized {
		if reason := tokenRefusal(resp.Header); reason != "" {
			serverErr = TokenRefusedError{Reason: reason, Endpoint: client.Endpoint}
		}
	}
	return Error{StatusCode: resp.StatusCode, ServerError: serverErr}
}

// tokenRefusal returns a WWW-Authenticate reason saying the token expired or
// was revoked, or "".
func tokenRefusal(header http.Header) string {
	for _, challenge := range header.Values("WWW-Authenticate") {
		m := challengeDescription.FindStringSubmatch(challenge)
		if m != nil && (strings.Contains(m[1], " expired at ") || strings.Contains(m[1], " was revoked at ")) {
			return m[1]
		}
	}
	return ""
}
