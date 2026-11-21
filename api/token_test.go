package api

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

type expiryCall struct {
	Endpoint string
	Expires  time.Time
}

// recordExpiry captures OnTokenExpiry calls for one test.
func recordExpiry(t *testing.T) *[]expiryCall {
	t.Helper()
	var calls []expiryCall
	prev := OnTokenExpiry
	OnTokenExpiry = func(endpoint string, expires time.Time) {
		calls = append(calls, expiryCall{endpoint, expires})
	}
	t.Cleanup(func() { OnTokenExpiry = prev })
	return &calls
}

func newHeaderTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Client{Endpoint: srv.URL, http: &http.Client{}, stream: &http.Client{}}
}

func TestTokenExpiry_Reported(t *testing.T) {
	expires := time.Date(2027, 2, 18, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		Name   string
		Header string
		Status int
		Call   func(*Client) error
		Want   bool
	}{
		{"Request", "2027-02-18T10:00:00Z", http.StatusOK, func(c *Client) error { _, err := c.UserInfo(); return err }, true},
		{"FailedRequest", "2027-02-18T10:00:00Z", http.StatusNotFound, func(c *Client) error { _, err := c.UserInfo(); return err }, true},
		{"Stream", "2027-02-18T10:00:00Z", http.StatusOK, func(c *Client) error { return c.StreamServiceLogs("p", "s", io.Discard, LogOptions{}) }, true},
		{"NoHeader", "", http.StatusOK, func(c *Client) error { _, err := c.UserInfo(); return err }, false},
		{"Malformed", "next week", http.StatusOK, func(c *Client) error { _, err := c.UserInfo(); return err }, false},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			calls := recordExpiry(t)
			client := newHeaderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.Header != "" {
					w.Header().Set(TokenExpiresHeader, tc.Header)
				}
				w.WriteHeader(tc.Status)
				w.Write([]byte(`{"error":"not found"}`))
			})
			tc.Call(client)
			switch {
			case !tc.Want && len(*calls) != 0:
				t.Errorf("reported %v, want nothing", *calls)
			case tc.Want && (len(*calls) != 1 || (*calls)[0] != expiryCall{client.Endpoint, expires}):
				t.Errorf("reported %v, want one call with %s", *calls, expires)
			}
		})
	}
}

func TestTokenRefused(t *testing.T) {
	const (
		expired = "token 3f2a... expired at 2027-02-18T10:00:00Z"
		revoked = "token 3f2a... was revoked at 2026-11-21T10:00:00Z"
	)
	challenge := func(reason string) string {
		return `Bearer error="invalid_token", error_description="` + reason + `"`
	}
	for _, tc := range []struct {
		Name        string
		Status      int
		Challenge   string
		Body        string
		WantErr     string
		WantRefused bool
	}{
		{"Expired", http.StatusUnauthorized, challenge(expired), `{"error":"token 3f2a… expired at 2027-02-18T10:00:00Z"}`, "Unauthorized: " + expired, true},
		{"Revoked", http.StatusUnauthorized, challenge(revoked), `{"error":"token 3f2a… was revoked at 2026-11-21T10:00:00Z"}`, "Unauthorized: " + revoked, true},
		{"ExpiredWithoutJSONBody", http.StatusUnauthorized, challenge(expired), "Unauthorized\n", "Unauthorized: " + expired, true},
		{"Unknown", http.StatusUnauthorized, challenge("unknown token"), `{"error":"unknown token"}`, "Unauthorized: unknown token", false},
		{"NoChallenge", http.StatusUnauthorized, "", `{"error":"invalid credentials"}`, "Unauthorized: invalid credentials", false},
		{"NotUnauthorized", http.StatusForbidden, challenge(expired), `{"error":"forbidden"}`, "Forbidden: forbidden", false},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			client := newHeaderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.Challenge != "" {
					w.Header().Set("WWW-Authenticate", tc.Challenge)
				}
				w.WriteHeader(tc.Status)
				w.Write([]byte(tc.Body))
			})
			for name, err := range map[string]error{
				"request": func() error { _, err := client.UserInfo(); return err }(),
				"stream":  client.StreamServiceLogs("p", "s", io.Discard, LogOptions{}),
			} {
				var apiErr Error
				if !errors.As(err, &apiErr) || apiErr.StatusCode != tc.Status {
					t.Fatalf("%s: got %v, want a %d API error", name, err, tc.Status)
				}
				if err.Error() != tc.WantErr {
					t.Errorf("%s: error = %q, want %q", name, err.Error(), tc.WantErr)
				}
				var refused TokenRefusedError
				if errors.As(err, &refused) != tc.WantRefused {
					t.Errorf("%s: refused = %v, want %v", name, !tc.WantRefused, tc.WantRefused)
				}
				if tc.WantRefused && refused.Endpoint != client.Endpoint {
					t.Errorf("%s: endpoint = %q, want %q", name, refused.Endpoint, client.Endpoint)
				}
			}
		})
	}
}

const (
	rootID = "0123456789abcdef0123456789abcdef"
	ciID   = "fedcba9876543210fedcba9876543210"
)

func TestTokenRequests(t *testing.T) {
	created := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	expires := created.Add(2160 * time.Hour)
	ci := `{"id":"` + ciID + `","kind":"user","name":"ci","project":"acme","scopes":["deploy","read"],"parent":"` + rootID +
		`","createdAt":"2026-11-21T10:00:00Z","expiresAt":"2027-02-19T10:00:00Z"`
	client, requests := newEnvTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /tokens":
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(ci + `,"value":"valar_ut_secret"}`))
		case "GET /tokens":
			w.Write([]byte(`[` + ci + `}]`))
		case "GET /projects/acme/tokens":
			w.Write([]byte(`[` + ci + `,"owner":"alice"}]`))
		case "DELETE /tokens/" + ciID, "DELETE /projects/acme/tokens/" + ciID:
			w.Write([]byte(`{"revoked":2}`))
		default:
			http.NotFound(w, r)
		}
	})
	want := Token{
		ID: ciID, Kind: "user", Name: "ci", Project: "acme", Scopes: []string{"deploy", "read"}, Parent: rootID,
		CreatedAt: created, ExpiresAt: expires,
	}

	minted, err := client.CreateToken(TokenRequest{Name: "ci", Project: "acme", Scopes: []string{"read", "deploy"}, TTL: "2160h"})
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if !reflect.DeepEqual(*minted, MintedToken{Token: want, Value: "valar_ut_secret"}) {
		t.Errorf("minted %+v", *minted)
	}
	own, err := client.ListTokens("")
	if err != nil || len(own) != 1 || !reflect.DeepEqual(own[0], want) {
		t.Errorf("ListTokens(\"\") = %+v, %v", own, err)
	}
	project, err := client.ListTokens("acme")
	if err != nil || len(project) != 1 || project[0].Owner != "alice" {
		t.Errorf("ListTokens(acme) = %+v, %v", project, err)
	}
	for _, p := range []string{"", "acme"} {
		if n, err := client.RevokeToken(p, ciID); err != nil || n != 2 {
			t.Errorf("RevokeToken(%q) = %d, %v", p, n, err)
		}
	}

	wantRequests := []recordedRequest{
		{http.MethodPost, "/tokens", `{"name":"ci","project":"acme","scopes":["read","deploy"],"ttl":"2160h"}`},
		{http.MethodGet, "/tokens", ""},
		{http.MethodGet, "/projects/acme/tokens", ""},
		{http.MethodDelete, "/tokens/" + ciID, ""},
		{http.MethodDelete, "/projects/acme/tokens/" + ciID, ""},
	}
	if !reflect.DeepEqual(*requests, wantRequests) {
		t.Errorf("requests\n got %+v\nwant %+v", *requests, wantRequests)
	}
}

func TestCreateToken_OmitsUnsetFields(t *testing.T) {
	client, requests := newEnvTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"kind":"user","scopes":["all"],"value":"v"}`))
	})
	if _, err := client.CreateToken(TokenRequest{Name: "laptop"}); err != nil {
		t.Fatal(err)
	}
	if got, want := (*requests)[0].Body, `{"name":"laptop"}`; got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

// The router answers unknown routes in plain text; the token routes answer in JSON.
func TestTokenRoutes_Errors(t *testing.T) {
	calls := map[string]func(*Client) error{
		"create": func(c *Client) error { _, err := c.CreateToken(TokenRequest{Name: "ci"}); return err },
		"list":   func(c *Client) error { _, err := c.ListTokens(""); return err },
		"revoke": func(c *Client) error { _, err := c.RevokeToken("acme", ciID); return err },
	}
	for _, tc := range []struct {
		Name    string
		Status  int
		Body    string
		WantOld bool
		WantErr string
	}{
		{"UnknownRoute", http.StatusNotFound, "404 page not found\n", true, ErrNoTokenAPI.Error()},
		{"UnknownMethod", http.StatusMethodNotAllowed, "", true, ErrNoTokenAPI.Error()},
		{"NoSuchToken", http.StatusNotFound, `{"error":"token not found"}`, false, "Not Found: token not found"},
		{
			"ChildMints", http.StatusForbidden, `{"error":"only tokens minted in the console or with authctl can mint tokens"}`, false,
			"Forbidden: only tokens minted in the console or with authctl can mint tokens",
		},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			client, _ := newEnvTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.Status)
				w.Write([]byte(tc.Body))
			})
			for name, call := range calls {
				err := call(client)
				if got := errors.Is(err, ErrNoTokenAPI); got != tc.WantOld {
					t.Errorf("%s: errors.Is(%v, ErrNoTokenAPI) = %v, want %v", name, err, got, tc.WantOld)
				}
				if err == nil || err.Error() != tc.WantErr {
					t.Errorf("%s: error = %v, want %q", name, err, tc.WantErr)
				}
			}
		})
	}
}

func TestUserInfo_Token(t *testing.T) {
	for _, tc := range []struct {
		Name string
		Body string
		Want *Token
	}{
		{"Grant", `{"name":"alice","projects":["acme"],"principal":"human::alice","token":{"id":"` + ciID +
			`","kind":"user","name":"ci","project":"acme","scopes":["read"],"parent":"` + rootID +
			`","createdAt":"2026-11-21T10:00:00Z","lastUsed":"2026-11-21T11:00:00Z"}}`, &Token{
			ID: ciID, Kind: "user", Name: "ci", Project: "acme", Scopes: []string{"read"}, Parent: rootID,
			CreatedAt: time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC), LastUsed: time.Date(2026, 11, 21, 11, 0, 0, 0, time.UTC),
		}},
		{"OlderServer", `{"name":"alice","projects":["acme"]}`, nil},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			client, _ := newEnvTestServer(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(tc.Body)) })
			info, err := client.UserInfo()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(info.Token, tc.Want) {
				t.Errorf("token = %+v, want %+v", info.Token, tc.Want)
			}
		})
	}
}
