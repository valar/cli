package cmd

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/valar/cli/api"
)

const (
	valarHint = "Create a new token at https://console.valar.dev/api-tokens/new and use it with 'valar config endpoint set <endpoint> --token <token>' or VALAR_TOKEN."
	otherHint = "Create a new token in the Valar console and use it with 'valar config endpoint set <endpoint> --token <token>' or VALAR_TOKEN."
	valarAPI  = "https://api.valar.dev/v2"
	hostedAPI = "https://valar.example.com/v2"
	day       = 24 * time.Hour
)

func TestExpiryNotice(t *testing.T) {
	now := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		Name     string
		Endpoint string
		Expires  []time.Time
		Want     string
	}{
		{"WithinWindow", valarAPI, []time.Time{now.Add(6 * day)}, "Warning: your Valar token expires in 6 days, at 2026-11-27 10:00 UTC. " + valarHint + "\n"},
		{"OncePerInvocation", valarAPI, []time.Time{now.Add(3 * time.Hour), now.Add(3 * time.Hour), now.Add(time.Hour)}, "Warning: your Valar token expires in 3 hours, at 2026-11-21 13:00 UTC. " + valarHint + "\n"},
		{"BeyondWindow", valarAPI, []time.Time{now.Add(15 * day)}, ""},
		{"BeyondThenWithin", valarAPI, []time.Time{now.Add(15 * day), now.Add(14 * day)}, "Warning: your Valar token expires in 14 days, at 2026-12-05 10:00 UTC. " + valarHint + "\n"},
		{"OtherEndpoint", hostedAPI, []time.Time{now.Add(13 * day)}, "Warning: your Valar token expires in 13 days, at 2026-12-04 10:00 UTC. " + otherHint + "\n"},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			var out bytes.Buffer
			notice := &expiryNotice{w: &out, now: func() time.Time { return now }}
			for _, expires := range tc.Expires {
				notice.observe(tc.Endpoint, expires)
			}
			if out.String() != tc.Want {
				t.Errorf("got %q, want %q", out.String(), tc.Want)
			}
		})
	}
}

func TestUntilExpiry(t *testing.T) {
	for _, tc := range []struct {
		Left time.Duration
		Want string
	}{
		{5*day - time.Second, "in 5 days"},
		{2 * day, "in 2 days"},
		{47 * time.Hour, "in 47 hours"},
		{2 * time.Hour, "in 2 hours"},
		{90 * time.Minute, "in 90 minutes"},
		{time.Minute, "in 1 minute"},
		{10 * time.Second, "within a minute"},
		{-time.Minute, "within a minute"},
	} {
		t.Run(tc.Want, func(t *testing.T) {
			if got := untilExpiry(tc.Left); got != tc.Want {
				t.Errorf("untilExpiry(%s) = %q, want %q", tc.Left, got, tc.Want)
			}
		})
	}
}

func TestExpiryNotice_OnceAcrossRequests(t *testing.T) {
	const token = "valar_ut_0123456789abcdef"
	expires := time.Now().Add(3 * day).UTC().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(api.TokenExpiresHeader, expires.Format(time.RFC3339))
		if r.URL.Path == "/" {
			w.Write([]byte(`{"version":"v2"}`))
			return
		}
		w.Write([]byte(`{"name":"ada"}`))
	}))
	t.Cleanup(srv.Close)
	var out bytes.Buffer
	prev := api.OnTokenExpiry
	api.OnTokenExpiry = (&expiryNotice{w: &out, now: time.Now}).observe
	t.Cleanup(func() { api.OnTokenExpiry = prev })

	client, err := api.NewClient(srv.URL, token)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	for range 3 {
		if _, err := client.UserInfo(); err != nil {
			t.Fatalf("UserInfo: %v", err)
		}
	}
	if n := strings.Count(out.String(), "Warning:"); n != 1 {
		t.Errorf("warned %d times, want once: %q", n, out.String())
	}
	if !strings.Contains(out.String(), expires.Format("2006-01-02 15:04 MST")) {
		t.Errorf("warning %q lacks the expiry", out.String())
	}
	if strings.Contains(out.String(), token) {
		t.Errorf("warning %q prints the token", out.String())
	}
}

func TestReportError_TokenRefused(t *testing.T) {
	const reason = "token 3f2a... expired at 2027-02-18T10:00:00Z"
	refused := fmt.Errorf("listing services: %w", api.Error{
		StatusCode:  http.StatusUnauthorized,
		ServerError: api.TokenRefusedError{Reason: reason, Endpoint: valarAPI},
	})
	for _, tc := range []struct {
		Name string
		JSON bool
		Want string
	}{
		{"Human", false, "listing services: Unauthorized: " + reason + "\n" + valarHint + "\n"},
		{"JSON", true, `{"error":{"kind":"notAllowed","message":"listing services: Unauthorized: ` + reason + `\n` + valarHint + `"}}` + "\n"},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			setJSON(t, tc.JSON)
			var stderr bytes.Buffer
			if code := reportError(&stderr, refused, kindFailed, false); code != int(kindNotAllowed) {
				t.Errorf("code %d, want %d", code, kindNotAllowed)
			}
			if stderr.String() != tc.Want {
				t.Errorf("stderr %q, want %q", stderr.String(), tc.Want)
			}
		})
	}
}
