package api

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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
		{"Stream", "2027-02-18T10:00:00Z", http.StatusOK, func(c *Client) error { return c.StreamServiceLogs("p", "s", io.Discard, false, false, 0) }, true},
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
				"stream":  client.StreamServiceLogs("p", "s", io.Discard, false, false, 0),
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
