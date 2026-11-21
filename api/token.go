package api

import (
	"bytes"
	"encoding/json"
	"errors"
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

// ErrNoTokenAPI is returned when the server predates the token routes.
var ErrNoTokenAPI = errors.New("the server has no token API; it needs a newer Valar platform")

// Token is a token's grant, never its value. Owner is set only in a project's
// token list. Times the server leaves out are zero.
type Token struct {
	ID        string    `json:"id,omitempty"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name,omitempty"`
	Owner     string    `json:"owner,omitempty"`
	Project   string    `json:"project,omitempty"`
	Service   string    `json:"service,omitempty"`
	Scopes    []string  `json:"scopes"`
	Consumer  string    `json:"consumer,omitempty"`
	Parent    string    `json:"parent,omitempty"`
	CreatedAt time.Time `json:"createdAt,omitzero"`
	ExpiresAt time.Time `json:"expiresAt,omitzero"`
	LastUsed  time.Time `json:"lastUsed,omitzero"`
	RevokedAt time.Time `json:"revokedAt,omitzero"`
}

// MintedToken is a new token with its value, which the server returns only once.
type MintedToken struct {
	Token
	Value string `json:"value"`
}

// TokenRequest mints a token. TTL is a Go duration; empty means the server's default.
type TokenRequest struct {
	Name string `json:"name"`
	// Kind is user, the server's default when empty, or agent.
	Kind    string   `json:"kind,omitempty"`
	Project string   `json:"project,omitempty"`
	Service string   `json:"service,omitempty"`
	Scopes  []string `json:"scopes,omitempty"`
	TTL     string   `json:"ttl,omitempty"`
}

// tokensPath is the caller's tokens, or with project set the project's.
func tokensPath(project string) string {
	if project == "" {
		return "/tokens"
	}
	return "/projects/" + project + "/tokens"
}

func tokenError(err error) error {
	if unknownRoute(err) {
		return ErrNoTokenAPI
	}
	return err
}

// CreateToken mints a child of the calling token, which must have been created
// in the console or with authctl.
func (client *Client) CreateToken(req TokenRequest) (*MintedToken, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	var token MintedToken
	if err := client.request(http.MethodPost, "/tokens", &token, bytes.NewReader(payload)); err != nil {
		return nil, tokenError(err)
	}
	return &token, nil
}

// ListTokens returns the caller's tokens, or with project set the tokens bound
// to that project, revoked and expired ones included.
func (client *Client) ListTokens(project string) ([]Token, error) {
	var tokens []Token
	if err := client.request(http.MethodGet, tokensPath(project), &tokens, nil); err != nil {
		return nil, tokenError(err)
	}
	return tokens, nil
}

// RevokeToken revokes a token and the tokens it created, from the caller's
// tokens or with project set from the project's, and returns how many tokens
// it revoked.
func (client *Client) RevokeToken(project, id string) (int, error) {
	var resp struct {
		Revoked int `json:"revoked"`
	}
	if err := client.request(http.MethodDelete, tokensPath(project)+"/"+id, &resp, nil); err != nil {
		return 0, tokenError(err)
	}
	return resp.Revoked, nil
}
