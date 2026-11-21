package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valar/cli/api"
)

const (
	exchangePath = "/projects/acme/services/web/token-exchange"
	policyToken  = "valar_ut_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	exchanged    = `{"token":"` + policyToken + `","expiresAt":"2026-11-21T13:00:00Z"}`
)

// fakeJWT builds an unsigned token with claims, enough for identityHint.
func fakeJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"RS256"}`)) + "." + enc(payload) + ".c2ln"
}

// fileWith returns an open file holding content, standing in for stdin.
func fileWith(t *testing.T, content string) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestReadIDToken(t *testing.T) {
	file := filepath.Join(t.TempDir(), "id-token")
	if err := os.WriteFile(file, []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		Name     string
		File     string
		Stdin    string
		Want     string
		WantKind errorKind
	}{
		{Name: "File", File: file, Want: "from-file"},
		{Name: "Dash", File: "-", Stdin: "from-stdin\n", Want: "from-stdin"},
		{Name: "Stdin", Stdin: "from-stdin", Want: "from-stdin"},
		{Name: "MissingFile", File: file + ".missing", WantKind: kindInvalid},
		{Name: "EmptyStdin", Stdin: "\n", WantKind: kindInvalid},
		{Name: "TooLarge", Stdin: strings.Repeat("a", maxIDToken+1), WantKind: kindInvalid},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			t.Setenv(envIDTokenRequestURL, "")
			got, err := readIDToken(tc.File, "", valarAPI, fileWith(t, tc.Stdin))
			if tc.WantKind != 0 {
				if err == nil || classify(err, kindFailed) != tc.WantKind {
					t.Errorf("error %v of kind %v, want kind %v", err, classify(err, kindFailed), tc.WantKind)
				}
				return
			}
			if err != nil || got != tc.Want {
				t.Errorf("readIDToken = %q, %v; want %q", got, err, tc.Want)
			}
		})
	}
}

func TestReadIDToken_GitHubActions(t *testing.T) {
	for _, tc := range []struct {
		Name         string
		Audience     string
		Endpoint     string
		Status       int
		WantAudience string
		WantKind     errorKind
	}{
		{Name: "EndpointOrigin", Endpoint: valarAPI, Status: http.StatusOK, WantAudience: "https://api.valar.dev"},
		{Name: "OriginWithPort", Endpoint: "https://valar.example.com:8443/api/v2", Status: http.StatusOK, WantAudience: "https://valar.example.com:8443"},
		{Name: "Audience", Audience: "https://valar.example.com", Endpoint: valarAPI, Status: http.StatusOK, WantAudience: "https://valar.example.com"},
		{Name: "Refused", Endpoint: valarAPI, Status: http.StatusForbidden, WantAudience: "https://api.valar.dev", WantKind: kindUnavailable},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			var gotAudience, gotAuth, gotVersion string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAudience, gotAuth, gotVersion = r.URL.Query().Get("audience"), r.Header.Get("Authorization"), r.URL.Query().Get("api-version")
				w.WriteHeader(tc.Status)
				w.Write([]byte(`{"value":"from-github"}`))
			}))
			t.Cleanup(srv.Close)
			t.Setenv(envIDTokenRequestURL, srv.URL+"/idtoken?api-version=2.0")
			t.Setenv(envIDTokenRequestToken, "request-secret")
			got, err := readIDToken("", tc.Audience, tc.Endpoint, fileWith(t, "not read"))
			if gotAudience != tc.WantAudience || gotAuth != "bearer request-secret" || gotVersion != "2.0" {
				t.Errorf("GitHub got audience %q, Authorization %q, api-version %q", gotAudience, gotAuth, gotVersion)
			}
			if tc.WantKind != 0 {
				if err == nil || classify(err, kindFailed) != tc.WantKind {
					t.Errorf("error %v of kind %v, want kind %v", err, classify(err, kindFailed), tc.WantKind)
				}
				return
			}
			if err != nil || got != "from-github" {
				t.Errorf("readIDToken = %q, %v; want from-github", got, err)
			}
		})
	}
}

// newExchangeAPI serves the token exchange, refusing requests that carry a
// bearer token, and records the request bodies.
func newExchangeAPI(t *testing.T, status int, body string) (*api.Client, *[]string) {
	t.Helper()
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("%s %s carries Authorization", r.Method, r.URL.Path)
		}
		if r.URL.Path == "/" {
			w.Write([]byte(`{"version":"v2"}`))
			return
		}
		if r.Method+" "+r.URL.Path != "POST "+exchangePath {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	client, err := api.NewClient(srv.URL, "")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client, &bodies
}

func TestExchangeToken(t *testing.T) {
	for _, tc := range []struct {
		Name         string
		JSON         bool
		Actions      bool
		WantOut      string
		WantProgress string
	}{
		{Name: "Human", WantOut: policyToken + "\n", WantProgress: "Exchanged the identity token for a token for acme/web until 2026-11-21 13:00 UTC.\n"},
		{
			Name: "GitHubActions", Actions: true, WantOut: policyToken + "\n",
			WantProgress: "::add-mask::" + policyToken + "\nExchanged the identity token for a token for acme/web until 2026-11-21 13:00 UTC.\n",
		},
		{Name: "JSON", JSON: true},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			setJSON(t, tc.JSON)
			t.Setenv("GITHUB_ENV", "")
			t.Setenv("GITHUB_ACTIONS", "")
			if tc.Actions {
				t.Setenv("GITHUB_ACTIONS", "true")
			}
			client, bodies := newExchangeAPI(t, http.StatusOK, exchanged)
			var out, progress bytes.Buffer
			if err := exchangeToken(client, "acme", "web", "id.token.jwt", false, &out, &progress); err != nil {
				t.Fatalf("exchangeToken: %v", err)
			}
			if len(*bodies) != 1 || (*bodies)[0] != `{"idToken":"id.token.jwt"}` {
				t.Errorf("bodies %q, want the identity token", *bodies)
			}
			if tc.JSON {
				var doc api.ExchangedToken
				if err := json.Unmarshal(out.Bytes(), &doc); err != nil || doc.Token != policyToken || doc.ExpiresAt.IsZero() {
					t.Errorf("stdout %s decodes to %+v, %v", out.String(), doc, err)
				}
				return
			}
			if out.String() != tc.WantOut || progress.String() != tc.WantProgress {
				t.Errorf("stdout %q and progress %q, want %q and %q", out.String(), progress.String(), tc.WantOut, tc.WantProgress)
			}
		})
	}
}

func TestExchangeToken_Export(t *testing.T) {
	setJSON(t, false)
	env := filepath.Join(t.TempDir(), "github_env")
	if err := os.WriteFile(env, []byte("EARLIER=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_ENV", env)
	t.Setenv("GITHUB_ACTIONS", "true")
	client, _ := newExchangeAPI(t, http.StatusOK, exchanged)
	var out, progress bytes.Buffer
	if err := exchangeToken(client, "acme", "web", "jwt", true, &out, &progress); err != nil {
		t.Fatalf("exchangeToken: %v", err)
	}
	data, _ := os.ReadFile(env)
	if string(data) != "EARLIER=1\nVALAR_TOKEN="+policyToken+"\n" {
		t.Errorf("GITHUB_ENV holds %q", data)
	}
	if out.Len() != 0 {
		t.Errorf("stdout %q, want nothing when exporting", out.String())
	}
	if !strings.HasPrefix(progress.String(), "::add-mask::"+policyToken+"\n") || !strings.Contains(progress.String(), "Set VALAR_TOKEN") {
		t.Errorf("progress %q, want the mask and a note", progress.String())
	}
}

func TestExchangeToken_InvalidToken(t *testing.T) {
	for name, token := range map[string]string{
		"Injection": policyToken + "\nBASH_ENV=/tmp/evil",
		"Short":     "valar_ut_0123",
		"AgentKind": strings.Replace(policyToken, "valar_ut_", "valar_at_", 1),
		"Empty":     "",
	} {
		t.Run(name, func(t *testing.T) {
			for _, export := range []bool{false, true} {
				setJSON(t, false)
				env := filepath.Join(t.TempDir(), "github_env")
				if err := os.WriteFile(env, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("GITHUB_ENV", env)
				t.Setenv("GITHUB_ACTIONS", "true")
				body, _ := json.Marshal(map[string]string{"token": token, "expiresAt": "2026-11-21T13:00:00Z"})
				client, _ := newExchangeAPI(t, http.StatusOK, string(body))
				var out, progress bytes.Buffer
				err := exchangeToken(client, "acme", "web", "jwt", export, &out, &progress)
				if err == nil || classify(err, kindFailed) != kindUnavailable {
					t.Errorf("export %v: error %v, want unavailable", export, err)
				}
				data, _ := os.ReadFile(env)
				if out.Len() != 0 || progress.Len() != 0 || len(data) != 0 {
					t.Errorf("export %v: printed %q and %q and exported %q", export, out.String(), progress.String(), data)
				}
			}
		})
	}
}

func TestExchangeToken_ExportOutsideActions(t *testing.T) {
	t.Setenv("GITHUB_ENV", "")
	client, bodies := newExchangeAPI(t, http.StatusOK, exchanged)
	err := exchangeToken(client, "acme", "web", "jwt", true, io.Discard, io.Discard)
	if err == nil || classify(err, kindFailed) != kindInvalid || len(*bodies) != 0 {
		t.Errorf("error %v after %d exchanges, want an invalid flag and none", err, len(*bodies))
	}
}

func TestExchangeToken_Refused(t *testing.T) {
	jwt := fakeJWT(t, map[string]any{
		"iss": api.GitHubIssuer, "sub": "repo:acme/web:ref:refs/heads/dev", "ref": "refs/heads/dev",
		"repository_id": "123", "event_name": "push", "run_id": "99", "aud": "https://api.valar.dev",
	})
	for _, tc := range []struct {
		Name     string
		Status   int
		Body     string
		IDToken  string
		WantKind errorKind
		Want     []string
		NotWant  []string
	}{
		{
			Name: "NoPolicy", Status: http.StatusForbidden, Body: `{"error":"no trust policy admits this identity for acme/web"}`, IDToken: jwt,
			WantKind: kindNotAllowed,
			Want: []string{
				"no trust policy admits this identity for acme/web", "The identity token carries:",
				"sub: repo:acme/web:ref:refs/heads/dev", "ref: refs/heads/dev", "repository_id: 123", "event_name: push",
			},
			NotWant: []string{"run_id", jwt},
		},
		{Name: "OpaqueToken", Status: http.StatusForbidden, Body: `{"error":"no trust policy admits this identity for acme/web"}`, IDToken: "opaque", WantKind: kindNotAllowed, NotWant: []string{"carries"}},
		{Name: "Expired", Status: http.StatusUnauthorized, Body: `{"error":"token expired"}`, IDToken: jwt, WantKind: kindNotAllowed, Want: []string{"token expired"}, NotWant: []string{"carries", "Create a new token"}},
		{Name: "Replayed", Status: http.StatusConflict, Body: `{"error":"identity token already exchanged"}`, IDToken: jwt, WantKind: kindConflict, Want: []string{"already exchanged"}},
		{Name: "Disabled", Status: http.StatusNotFound, Body: `{"error":"deploys from CI are not enabled on this installation"}`, IDToken: jwt, WantKind: kindNotFound, Want: []string{"not enabled"}},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			setJSON(t, false)
			t.Setenv("GITHUB_ENV", "")
			client, _ := newExchangeAPI(t, tc.Status, tc.Body)
			var out, progress bytes.Buffer
			err := exchangeToken(client, "acme", "web", tc.IDToken, false, &out, &progress)
			if err == nil || classify(err, kindFailed) != tc.WantKind {
				t.Fatalf("error %v of kind %v, want kind %v", err, classify(err, kindFailed), tc.WantKind)
			}
			var report bytes.Buffer
			reportError(&report, err, kindFailed, false)
			containsAll(t, "error", report.String(), tc.Want...)
			for _, unwanted := range tc.NotWant {
				if strings.Contains(report.String(), unwanted) {
					t.Errorf("error %q contains %q", report.String(), unwanted)
				}
			}
			if out.Len() != 0 || progress.Len() != 0 {
				t.Errorf("printed %q and %q on a refusal", out.String(), progress.String())
			}
		})
	}
}

func TestExchangeToken_OlderServer(t *testing.T) {
	client, _ := newFakeAPI(t, nil)
	if err := exchangeToken(client, "acme", "web", "jwt", false, io.Discard, io.Discard); !errors.Is(err, api.ErrNoTrustAPI) {
		t.Errorf("error = %v, want ErrNoTrustAPI", err)
	}
}

func TestAuthExchangeCommand(t *testing.T) {
	writeServiceConfig(t, deployConfig)
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("%s carries Authorization", r.URL.Path)
		}
		if r.URL.Path == "/" {
			w.Write([]byte(`{"version":"v2"}`))
			return
		}
		gotPath = r.URL.Path
		w.Write([]byte(exchanged))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv(envIDTokenRequestURL, "")
	t.Cleanup(func() { exchangeService, exchangeProject, exchangeIDTokenFile = "", "", "" })
	idToken := filepath.Join(t.TempDir(), "id-token")
	if err := os.WriteFile(idToken, []byte("jwt"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := runCLI(t, srv.URL, "auth", "exchange", "--service", "api", "--id-token-file", idToken)
	if gotPath != "/projects/p/services/api/token-exchange" {
		t.Errorf("exchanged at %q, want the project of .valar.yml and --service", gotPath)
	}
	if !strings.Contains(out, `"token": "`+policyToken+`"`) {
		t.Errorf("output %q lacks the token", out)
	}
}
