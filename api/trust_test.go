package api

import (
	"errors"
	"net/http"
	"testing"
)

func TestAuthorization(t *testing.T) {
	for _, tc := range []struct {
		Name  string
		Token string
		Call  func(*Client) error
		Want  string
	}{
		{"Token", "valar_ut_x", func(c *Client) error { _, err := c.UserInfo(); return err }, "Bearer valar_ut_x"},
		{"NoToken", "", func(c *Client) error { _, err := c.UserInfo(); return err }, ""},
		{"Exchange", "valar_ut_x", func(c *Client) error { _, err := c.ExchangeToken("acme", "web", "jwt"); return err }, ""},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			var got []string
			client := newHeaderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Values("Authorization")
				w.Write([]byte(`{}`))
			})
			client.Token = tc.Token
			if err := tc.Call(client); err != nil {
				t.Fatal(err)
			}
			if tc.Want == "" && len(got) != 0 || tc.Want != "" && (len(got) != 1 || got[0] != tc.Want) {
				t.Errorf("Authorization %q, want %q", got, tc.Want)
			}
		})
	}
}

func TestTrustRoutes_OlderServer(t *testing.T) {
	client := newHeaderTestClient(t, http.NotFound)
	for name, call := range map[string]func() error{
		"create":   func() error { _, err := client.CreateTrustPolicy("acme", TrustPolicyRequest{}); return err },
		"list":     func() error { _, err := client.ListTrustPolicies("acme"); return err },
		"delete":   func() error { _, err := client.DeleteTrustPolicy("acme", "x"); return err },
		"exchange": func() error { _, err := client.ExchangeToken("acme", "web", "jwt"); return err },
	} {
		if err := call(); !errors.Is(err, ErrNoTrustAPI) {
			t.Errorf("%s: error = %v, want ErrNoTrustAPI", name, err)
		}
	}
	if _, err := client.InspectService("acme", "web"); !errors.Is(err, ErrNoInspectAPI) {
		t.Errorf("inspect: error = %v, want ErrNoInspectAPI", err)
	}
}

func TestInspectService(t *testing.T) {
	var path string
	client := newHeaderTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Write([]byte(`{"name":"web","kind":"function","domains":["acme-web.valar.app"],"urls":["https://acme-web.valar.app"]}`))
	})
	svc, err := client.InspectService("acme", "web")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/projects/acme/services/web/inspect" || svc.Name != "web" || len(svc.URLs) != 1 || svc.URLs[0] != "https://acme-web.valar.app" {
		t.Errorf("GET %s gave %+v", path, svc)
	}
}
