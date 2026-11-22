package cmd

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVerifyToken(t *testing.T) {
	for _, tc := range []struct {
		Name     string
		Status   int
		Info     string
		WantWarn string
		WantKind errorKind
		WantErr  string
	}{
		{Name: "User", Info: `{"name":"alice","projects":[],"token":{"kind":"user","scopes":["all"],"expiresAt":"2027-01-20T12:00:00Z"}}`},
		{
			Name: "ExpiresSoon", Info: `{"name":"alice","projects":[],"token":{"kind":"user","scopes":["all"],"expiresAt":"2026-11-24T12:00:00Z"}}`,
			WantWarn: "Warning: your Valar token expires in 3 days, at 2026-11-24 12:00 UTC.",
		},
		{Name: "NoExpiry", Info: `{"name":"alice","projects":[],"token":{"kind":"user","scopes":["all"]}}`},
		{Name: "UnknownKind", Info: `{"name":"alice","projects":[],"token":{"kind":"future-kind","scopes":["all"],"expiresAt":"2027-01-20T12:00:00Z"}}`},
		{
			Name: "Agent", Info: `{"name":"alice","projects":[],"token":{"kind":"agent","project":"acme","scopes":["read"]}}`,
			WantKind: kindNotAllowed, WantErr: "this is an agent token",
		},
		{Name: "OlderPlatform", Info: `{"name":"alice","projects":["acme"]}`},
		{Name: "Unknown", Status: http.StatusUnauthorized, Info: `{"error":"unknown token"}`, WantKind: kindNotAllowed, WantErr: "Unauthorized: unknown token"},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			statuses := map[string]int{}
			if tc.Status != 0 {
				statuses["GET /users/info"] = tc.Status
			}
			client, requests := newFakeAPIWithStatus(t, map[string]string{"GET /users/info": tc.Info}, statuses)
			var warning bytes.Buffer
			notice := &expiryNotice{w: &warning, now: func() time.Time { return tokenNow }}
			err := verifyToken(client.Endpoint, "token", notice.observe)
			if got := routeOrder(*requests); len(got) != 1 || got[0] != "GET /users/info" {
				t.Errorf("requests %v, want [GET /users/info]", got)
			}
			if tc.WantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.WantErr) || classify(err, kindFailed) != tc.WantKind {
					t.Errorf("error %v, want kind %v containing %q", err, tc.WantKind, tc.WantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("verifyToken: %v", err)
			}
			if !strings.HasPrefix(warning.String(), tc.WantWarn) || (tc.WantWarn == "") != (warning.Len() == 0) {
				t.Errorf("warning %q, want %q", warning.String(), tc.WantWarn)
			}
		})
	}
}

// runConfigCLI runs the root command with a fresh home directory and no
// VALAR_* variables, and returns the error and the default config path.
func runConfigCLI(t *testing.T, args ...string) (error, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{"VALARCONFIG", "VALAR_TOKEN", "VALAR_ENDPOINT", "VALAR_PROJECT"} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
	setJSON(t, false)
	silenceErrors, silenceUsage := rootCmd.SilenceErrors, rootCmd.SilenceUsage
	rootCmd.SilenceErrors, rootCmd.SilenceUsage = true, true
	t.Cleanup(func() {
		rootCmd.SilenceErrors, rootCmd.SilenceUsage = silenceErrors, silenceUsage
		configInitUrl, configInitToken, configInitProject, configInitName, configInitForce = "https://api.valar.dev/v2", "", "", "default", false
		configEndpointSetUrl, configEndpointSetToken = "", ""
		rootCmd.SetArgs(nil)
	})
	rootCmd.SetArgs(append(args, "--json"))
	return rootCmd.Execute(), filepath.Join(home, ".valar", "config")
}

func TestConfigCommands_StoreCheckedTokens(t *testing.T) {
	const (
		user  = `{"name":"alice","projects":["acme"],"token":{"kind":"user","scopes":["all"]}}`
		agent = `{"name":"alice","projects":["acme"],"token":{"kind":"agent","project":"acme","scopes":["read"]}}`
		older = `{"name":"alice","projects":["acme"]}`
	)
	for _, tc := range []struct {
		Name         string
		Info         string
		Args         func(endpoint string) []string
		WantRequests int
		WantKind     errorKind
	}{
		{"InitUser", user, func(e string) []string {
			return []string{"config", "init", "--url", e, "--token", "tok", "--project", "acme"}
		}, 1, 0},
		{"InitOlderPlatform", older, func(e string) []string {
			return []string{"config", "init", "--url", e, "--token", "tok", "--project", "acme"}
		}, 1, 0},
		{"InitAgent", agent, func(e string) []string {
			return []string{"config", "init", "--url", e, "--token", "tok", "--project", "acme"}
		}, 1, kindNotAllowed},
		{"EndpointSetAgent", agent, func(e string) []string {
			return []string{"config", "endpoint", "set", "prod", "--url", e, "--token", "tok"}
		}, 1, kindNotAllowed},
		{"EndpointSetUser", user, func(e string) []string {
			return []string{"config", "endpoint", "set", "prod", "--url", e, "--token", "tok"}
		}, 1, 0},
		// Without a URL there is nothing to ask yet.
		{"EndpointSetTokenOnly", agent, func(string) []string { return []string{"config", "endpoint", "set", "prod", "--token", "tok"} }, 0, 0},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			client, requests := newFakeAPI(t, map[string]string{"GET /users/info": tc.Info})
			err, path := runConfigCLI(t, tc.Args(client.Endpoint)...)
			if len(*requests) != tc.WantRequests {
				t.Errorf("requests %v, want %d", routeOrder(*requests), tc.WantRequests)
			}
			if tc.WantKind != 0 {
				if err == nil || classify(err, kindFailed) != tc.WantKind {
					t.Errorf("error %v, want kind %v", err, tc.WantKind)
				}
				if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
					t.Errorf("a refused token was stored in %s", path)
				}
				return
			}
			if err != nil {
				t.Fatalf("command failed: %v", err)
			}
			info, statErr := os.Stat(path)
			if statErr != nil {
				t.Fatalf("config not written: %v", statErr)
			}
			if info.Mode().Perm() != 0o600 {
				t.Errorf("config mode %o, want 600", info.Mode().Perm())
			}
			if data, _ := os.ReadFile(path); !strings.Contains(string(data), "token: tok") {
				t.Errorf("config %q lacks the token", data)
			}
		})
	}
}
