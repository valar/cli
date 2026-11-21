package cmd

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valar/cli/api"
	"github.com/valar/cli/config"
)

type recordedRequest struct {
	Method, Path, Body string
}

// newFakeAPI serves routes keyed by "METHOD /path" and answers anything else
// the way the platform's router answers an unknown route. It records every
// request except the client's version check.
func newFakeAPI(t *testing.T, routes map[string]string) (*api.Client, *[]recordedRequest) {
	t.Helper()
	return newFakeAPIWithStatus(t, routes, nil)
}

// newFakeAPIWithStatus is newFakeAPI answering the routes in statuses with
// that status code.
func newFakeAPIWithStatus(t *testing.T, routes map[string]string, statuses map[string]int) (*api.Client, *[]recordedRequest) {
	t.Helper()
	var requests []recordedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Write([]byte(`{"version":"v2"}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		requests = append(requests, recordedRequest{r.Method, r.URL.Path, string(body)})
		resp, ok := routes[r.Method+" "+r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if status, ok := statuses[r.Method+" "+r.URL.Path]; ok {
			w.WriteHeader(status)
		}
		w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	client, err := api.NewClient(srv.URL, "token")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client, &requests
}

// writeServiceConfig writes a .valar.yml into a fresh working directory.
func writeServiceConfig(t *testing.T, content string) (*config.ValidatedServiceConfig, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, functionConfiguration)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	cfg, err := config.NewServiceConfigFromFile(functionConfiguration)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	return cfg, path
}

func TestParseEnvAssignments(t *testing.T) {
	read := func(name string) (string, error) { return "from-stdin", nil }
	for _, tc := range []struct {
		Name    string
		Args    []string
		Want    map[string]string
		WantErr bool
	}{
		{"Single", []string{"A=1"}, map[string]string{"A": "1"}, false},
		{"Multiple", []string{"A=1", "B=2"}, map[string]string{"A": "1", "B": "2"}, false},
		{"ValueWithEquals", []string{"DSN=a=b"}, map[string]string{"DSN": "a=b"}, false},
		{"EmptyValue", []string{"A="}, map[string]string{"A": ""}, false},
		{"FromStdin", []string{"TOKEN"}, map[string]string{"TOKEN": "from-stdin"}, false},
		{"StdinTwice", []string{"A", "B"}, nil, true},
		{"Duplicate", []string{"A=1", "A=2"}, nil, true},
		{"NoName", []string{"=1"}, nil, true},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			got, err := parseEnvAssignments(tc.Args, read)
			if tc.WantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", tc.Args)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.Want) {
				t.Fatalf("got %v, want %v", got, tc.Want)
			}
			for k, v := range tc.Want {
				if got[k] != v {
					t.Errorf("%s: got %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestReadValue(t *testing.T) {
	for _, tc := range []struct{ In, Want string }{
		{"s3cret\n", "s3cret"},
		{"s3cret\r\n", "s3cret"},
		{"s3cret", "s3cret"},
		// Only the final line break goes: a PEM block keeps its lines.
		{"line1\nline2\n", "line1\nline2"},
	} {
		got, err := readValue(strings.NewReader(tc.In))
		if err != nil || got != tc.Want {
			t.Errorf("readValue(%q) = %q, %v, want %q", tc.In, got, err, tc.Want)
		}
	}
	// An empty stdin is far more likely a broken pipe than an intended value.
	if _, err := readValue(strings.NewReader("\n")); err == nil {
		t.Error("an empty value on stdin should be refused")
	}
}

func TestEnvTargets(t *testing.T) {
	for _, tc := range []struct {
		Build, All bool
		Want       string
	}{
		{false, false, "runtime"},
		{true, false, "build"},
		{false, true, "runtime,build"},
		{true, true, "runtime,build"},
	} {
		if got := strings.Join(envTargets(tc.Build, tc.All), ","); got != tc.Want {
			t.Errorf("envTargets(%v, %v) = %s, want %s", tc.Build, tc.All, got, tc.Want)
		}
	}
}

func TestFilterEnv(t *testing.T) {
	vars := []api.EnvVariable{
		{Name: "B", Target: "build"},
		{Name: "Z", Target: "runtime"},
		{Name: "A", Target: "runtime"},
		{Name: "A", Target: "build"},
	}
	var names []string
	for _, v := range filterEnv(vars, envTargets(false, true)) {
		names = append(names, v.Target+"/"+v.Name)
	}
	if got, want := strings.Join(names, " "), "runtime/A runtime/Z build/A build/B"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	if got := filterEnv(vars, envTargets(true, false)); len(got) != 2 || got[0].Name != "A" {
		t.Errorf("build only: got %+v", got)
	}
}

// The server never returns secret values; the CLI must not print one even if
// it did.
func TestPrintEnv_NeverPrintsSecretValues(t *testing.T) {
	vars := []api.EnvVariable{
		{Name: "PLAIN", Target: "runtime", Value: "visible"},
		{Name: "TOKEN", Target: "runtime", Secret: true, Value: "canary-s3cret"},
	}
	for _, format := range []string{"table", "raw"} {
		var out bytes.Buffer
		if err := printEnv(&out, vars, format); err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		if strings.Contains(out.String(), "canary-s3cret") {
			t.Errorf("%s output shows a secret value:\n%s", format, out.String())
		}
		if !strings.Contains(out.String(), "visible") {
			t.Errorf("%s output lacks the plain value:\n%s", format, out.String())
		}
	}
	var raw bytes.Buffer
	printEnv(&raw, vars, "raw")
	if raw.String() != "PLAIN=visible\n" {
		t.Errorf("raw output %q should hold only plain variables", raw.String())
	}
}

func TestSplitInlineList(t *testing.T) {
	plain, secrets := splitInlineList([]config.EnvironmentConfig{
		{Key: "A", Value: "1"},
		{Key: "S", Value: "v2:ct", Secret: true},
		{Key: "A", Value: "2"},
		// A later secret entry replaces a plain one of the same name.
		{Key: "B", Value: "plain"},
		{Key: "B", Value: "v2:ct", Secret: true},
	})
	if len(plain) != 1 || plain["A"] != "2" {
		t.Errorf("plain = %v, want A=2", plain)
	}
	if strings.Join(secrets, ",") != "B,S" {
		t.Errorf("secrets = %v, want [B S]", secrets)
	}
}

const importConfig = `project: p
service: s
build:
    constructor: node-static
    ignore:
        - .git
    environment:
        - key: INSTALL_CMD
          value: npm ci
          secret: false
        - key: NPM_TOKEN
          value: v2:abc
          secret: true
deployment:
    skip: false
    environment:
        - A=1
        - B=2
        - key: DB_PASSWORD
          value: v2:def
          secret: true
`

func TestImportEnv(t *testing.T) {
	cfg, path := writeServiceConfig(t, importConfig)
	client, requests := newFakeAPI(t, map[string]string{
		// The seed holds the runtime secret but not the build one.
		"GET /projects/p/services/s/env":   `[{"name":"DB_PASSWORD","target":"runtime","secret":true},{"name":"A","target":"runtime","secret":true}]`,
		"PATCH /projects/p/services/s/env": `{"skipped":[]}`,
	})
	var out bytes.Buffer
	if _, err := importEnv(client, cfg, &out); err != nil {
		t.Fatalf("importEnv: %v", err)
	}

	var patches []string
	for _, r := range *requests {
		if r.Method == http.MethodPatch {
			patches = append(patches, r.Body)
		}
	}
	want := []string{
		`{"set":{"A":{"value":"1","secret":false,"targets":["runtime"]},"B":{"value":"2","secret":false,"targets":["runtime"]}},"onlySeeded":true}`,
		`{"set":{"INSTALL_CMD":{"value":"npm ci","secret":false,"targets":["build"]}},"onlySeeded":true}`,
	}
	if strings.Join(patches, "\n") != strings.Join(want, "\n") {
		t.Errorf("PATCH bodies\n got %s\nwant %s", strings.Join(patches, "\n"), strings.Join(want, "\n"))
	}
	for _, r := range *requests {
		if strings.Contains(r.Body, "v2:") {
			t.Errorf("a ciphertext was sent: %s", r.Body)
		}
	}
	if !strings.Contains(out.String(), "build secrets NPM_TOKEN. Set each again with 'valar env set --secret --build NAME'") {
		t.Errorf("output does not name the missing build secret:\n%s", out.String())
	}
	if strings.Contains(out.String(), "DB_PASSWORD") {
		t.Errorf("a secret the service holds is reported missing:\n%s", out.String())
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reread, err := config.NewServiceConfigFromFile(functionConfiguration)
	if err != nil {
		t.Fatalf("reread: %v\n%s", err, written)
	}
	if n := len(reread.Build().Environment) + len(reread.Deployment().Environment); n != 0 {
		t.Errorf("%d entries left in the file:\n%s", n, written)
	}
	if strings.Count(string(written), "environment: []") != 2 {
		t.Errorf("both lists should remain, empty:\n%s", written)
	}
	if reread.Project() != "p" || reread.Service() != "s" || reread.Build().Constructor != "node-static" {
		t.Errorf("the rest of the file changed:\n%s", written)
	}
}

func TestImportEnv_ReportsSkipped(t *testing.T) {
	cfg, _ := writeServiceConfig(t, importConfig)
	client, _ := newFakeAPI(t, map[string]string{
		"GET /projects/p/services/s/env":   `[]`,
		"PATCH /projects/p/services/s/env": `{"skipped":["B"]}`,
	})
	var out bytes.Buffer
	if _, err := importEnv(client, cfg, &out); err != nil {
		t.Fatalf("importEnv: %v", err)
	}
	if !strings.Contains(out.String(), "Imported the plain runtime variables A.") || !strings.Contains(out.String(), "Left B as stored") {
		t.Errorf("output does not report the skipped name:\n%s", out.String())
	}
}

func TestImportEnv_OldServerChangesNothing(t *testing.T) {
	cfg, path := writeServiceConfig(t, importConfig)
	client, requests := newFakeAPI(t, nil)
	_, err := importEnv(client, cfg, io.Discard)
	if !errors.Is(err, api.ErrNoEnvAPI) {
		t.Fatalf("got %v, want ErrNoEnvAPI", err)
	}
	for _, r := range *requests {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected %s %s", r.Method, r.Path)
		}
	}
	if written, _ := os.ReadFile(path); string(written) != importConfig {
		t.Errorf("the file changed:\n%s", written)
	}
}

func TestImportEnv_NothingToImport(t *testing.T) {
	cfg, _ := writeServiceConfig(t, "project: p\nservice: s\nbuild:\n    environment: []\ndeployment:\n    environment: []\n")
	client, requests := newFakeAPI(t, nil)
	if _, err := importEnv(client, cfg, io.Discard); err != nil {
		t.Fatalf("importEnv: %v", err)
	}
	if len(*requests) != 0 {
		t.Errorf("unexpected requests %+v", *requests)
	}
}
