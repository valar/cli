package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/valar/cli/api"
	"github.com/valar/cli/config"
)

// setJSON switches JSON output on and captures stdout for one test.
func setJSON(t *testing.T, on bool) *bytes.Buffer {
	t.Helper()
	var out bytes.Buffer
	prevJSON, prevStdout := outputJSON, stdout
	outputJSON, stdout = on, &out
	t.Cleanup(func() { outputJSON, stdout = prevJSON, prevStdout })
	return &out
}

func TestClassify(t *testing.T) {
	apiErr := func(status int) error {
		return fmt.Errorf("listing services: %w", api.Error{StatusCode: status, ServerError: errors.New("x")})
	}
	for _, tc := range []struct {
		Name     string
		Err      error
		Fallback errorKind
		WantCode int
		WantKind string
	}{
		{"Untyped", errors.New("boom"), kindFailed, 1, "failed"},
		{"UntypedFromCobra", errors.New("unknown flag: --nope"), kindInvalid, 2, "invalid"},
		{"Typed", newError(kindConflict, "exists"), kindFailed, 5, "conflict"},
		{"TypedWinsOverAPI", withKind(kindFailed, apiErr(http.StatusNotFound)), kindFailed, 1, "failed"},
		{"BadRequest", apiErr(http.StatusBadRequest), kindFailed, 2, "invalid"},
		{"Unprocessable", apiErr(http.StatusUnprocessableEntity), kindFailed, 2, "invalid"},
		{"NotFound", apiErr(http.StatusNotFound), kindFailed, 3, "notFound"},
		{"Gone", apiErr(http.StatusGone), kindFailed, 3, "notFound"},
		{"Unauthorized", apiErr(http.StatusUnauthorized), kindFailed, 4, "notAllowed"},
		{"Forbidden", apiErr(http.StatusForbidden), kindFailed, 4, "notAllowed"},
		{"Conflict", apiErr(http.StatusConflict), kindFailed, 5, "conflict"},
		{"TooManyRequests", apiErr(http.StatusTooManyRequests), kindFailed, 6, "unavailable"},
		{"InternalError", apiErr(http.StatusInternalServerError), kindFailed, 6, "unavailable"},
		{"BadGateway", apiErr(http.StatusBadGateway), kindFailed, 6, "unavailable"},
		{"OtherClientError", apiErr(http.StatusMethodNotAllowed), kindFailed, 1, "failed"},
		{"NoEnvAPI", api.ErrNoEnvAPI, kindFailed, 1, "failed"},
		{"ServiceConfig", &config.Error{Path: ".valar.yml", Err: errors.New("unknown key")}, kindFailed, 2, "invalid"},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			kind := classify(tc.Err, tc.Fallback)
			if int(kind) != tc.WantCode || kind.String() != tc.WantKind {
				t.Errorf("classify = %d %s, want %d %s", kind, kind, tc.WantCode, tc.WantKind)
			}
		})
	}
}

func TestClassify_Unreachable(t *testing.T) {
	_, err := api.NewClient("http://127.0.0.1:1", "token")
	if err == nil {
		t.Fatal("expected a connection error")
	}
	if kind := classify(err, kindFailed); kind != kindUnavailable {
		t.Errorf("classify(%v) = %s, want unavailable", err, kind)
	}
}

func TestReportError(t *testing.T) {
	for _, tc := range []struct {
		Name    string
		JSON    bool
		Err     error
		Printed bool
		Want    string
	}{
		{"Human", false, newError(kindNotFound, "no builds available"), false, "no builds available\n"},
		{"HumanAlreadyPrinted", false, errors.New("unknown flag"), true, ""},
		{"HumanSilent", false, &cliError{kind: kindFailed, err: errors.New("x"), silent: true}, false, ""},
		{"JSON", true, newError(kindNotFound, "no builds available"), false, `{"error":{"kind":"notFound","message":"no builds available"}}` + "\n"},
		// cobra's own message is suppressed in JSON mode, so it is never printed.
		{"JSONSilent", true, &cliError{kind: kindFailed, err: errors.New("execution e1 failed"), silent: true}, false, `{"error":{"kind":"failed","message":"execution e1 failed"}}` + "\n"},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			setJSON(t, tc.JSON)
			var stderr bytes.Buffer
			code := reportError(&stderr, tc.Err, kindFailed, tc.Printed)
			if stderr.String() != tc.Want {
				t.Errorf("stderr %q, want %q", stderr.String(), tc.Want)
			}
			if want := int(classify(tc.Err, kindFailed)); code != want {
				t.Errorf("code %d, want %d", code, want)
			}
		})
	}
}

func TestJSONRequested(t *testing.T) {
	for _, tc := range []struct {
		Name string
		Args []string
		Env  string
		Want bool
	}{
		{"None", []string{"service", "list"}, "", false},
		{"Flag", []string{"service", "list", "--json"}, "", true},
		{"FlagValue", []string{"--json=true", "service", "list"}, "", true},
		{"Env", []string{"service", "list"}, "json", true},
		{"FlagOverridesEnv", []string{"--json=false"}, "json", false},
		{"OtherEnv", nil, "table", false},
		{"AfterDashDash", []string{"batch", "run", "--", "--json"}, "", false},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			if got := jsonRequested(tc.Args, tc.Env); got != tc.Want {
				t.Errorf("got %v, want %v", got, tc.Want)
			}
		})
	}
}

func TestLogOutput_JSON(t *testing.T) {
	out := setJSON(t, true)
	lw, flush := logOutput()
	lw.Write([]byte("first\nsec"))
	lw.Write([]byte("ond\r\n\"quoted\" <b>\nlast"))
	if err := flush(); err != nil {
		t.Fatal(err)
	}
	want := `{"line":"first"}
{"line":"second"}
{"line":"\"quoted\" <b>"}
{"line":"last"}
`
	if out.String() != want {
		t.Errorf("got\n%s\nwant\n%s", out.String(), want)
	}
}

func TestWriteJSON_NilSliceIsEmptyArray(t *testing.T) {
	var out bytes.Buffer
	var builds []api.Build
	if err := writeJSON(&out, builds); err != nil {
		t.Fatal(err)
	}
	if out.String() != "[]\n" {
		t.Errorf("got %q, want []", out.String())
	}
}

func TestListBuilds_JSON(t *testing.T) {
	cfg, _ := writeServiceConfig(t, deployConfig)
	client, _ := newFakeAPI(t, map[string]string{
		"GET /projects/p/services/s/builds/": `[
			{"id":"b2","constructor":"go","status":"failed","error":"exit 1","createdAt":"2026-11-20T10:00:00Z","flags":"","owner":"u"},
			{"id":"b1","constructor":"go","status":"done","error":"","createdAt":"2026-11-19T10:00:00Z","flags":"","owner":"u"}
		]`,
	})
	out := setJSON(t, true)
	if err := listBuilds(client, cfg, ""); err != nil {
		t.Fatalf("listBuilds: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output is not one JSON array: %v\n%s", err, out)
	}
	if len(got) != 2 || got[0]["id"] != "b1" || got[1]["status"] != "failed" || got[1]["error"] != "exit 1" {
		t.Errorf("got %v, want both builds oldest first", got)
	}
	for _, key := range []string{"id", "constructor", "status", "error", "createdAt", "flags", "owner"} {
		if _, ok := got[0][key]; !ok {
			t.Errorf("build lacks field %q: %v", key, got[0])
		}
	}
}

func TestListBuilds_HumanUnchanged(t *testing.T) {
	cfg, _ := writeServiceConfig(t, deployConfig)
	client, _ := newFakeAPI(t, map[string]string{"GET /projects/p/services/s/builds/": `[{"id":"b1","status":"done"}]`})
	out := setJSON(t, false)
	if err := listBuilds(client, cfg, ""); err != nil {
		t.Fatalf("listBuilds: %v", err)
	}
	if !strings.HasPrefix(out.String(), "ID") || strings.Contains(out.String(), "{") {
		t.Errorf("human output changed: %q", out.String())
	}
}

func TestInspectBuild_JSON(t *testing.T) {
	cfg, _ := writeServiceConfig(t, deployConfig)
	client, _ := newFakeAPI(t, map[string]string{
		"GET /projects/p/services/s/builds/b1/inspect": `{"id":"b1","constructor":"go","status":"done","createdAt":"2026-11-20T10:00:00Z"}`,
	})
	out := setJSON(t, true)
	if err := inspectBuild(client, cfg, "b1"); err != nil {
		t.Fatalf("inspectBuild: %v", err)
	}
	var got api.Build
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output is not one JSON object: %v\n%s", err, out)
	}
	if got.ID != "b1" || got.Status != "done" || got.Constructor != "go" {
		t.Errorf("got %+v", got)
	}
}

func TestListDeployments_JSON(t *testing.T) {
	cfg, _ := writeServiceConfig(t, deployConfig)
	client, _ := newFakeAPI(t, map[string]string{
		listRoute: `[{"version":2,"status":"done","build":"b2"},{"version":1,"status":"expired","build":"b1"}]`,
	})
	out := setJSON(t, true)
	if err := listDeployments(client, cfg); err != nil {
		t.Fatalf("listDeployments: %v", err)
	}
	var got []api.Deployment
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output is not one JSON array: %v\n%s", err, out)
	}
	if len(got) != 2 || got[0].Version != 1 || got[1].Build != "b2" {
		t.Errorf("got %+v, want both deployments by version", got)
	}
}

func TestDeployBuild_JSON(t *testing.T) {
	cfg, _ := writeServiceConfig(t, deployConfig)
	client, _ := newFakeAPI(t, map[string]string{deployRoute: `{"version":7,"status":"scheduled","build":"b1"}`, envRoute: `[]`})
	setJSON(t, true)
	var out bytes.Buffer
	if err := deployBuild(client, cfg, "b1", &out); err != nil {
		t.Fatalf("deployBuild: %v", err)
	}
	var got api.Deployment
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got.Version != 7 || got.Build != "b1" {
		t.Errorf("got %+v (%v) from %s", got, err, out.String())
	}
}

func TestCreateProject_JSON(t *testing.T) {
	client, _ := newFakeAPI(t, map[string]string{"POST /projects": `{"name":"demo","creator":"user:kc/alice","public":true}`})
	setJSON(t, true)
	var out bytes.Buffer
	if err := createProject(client, "demo", true, &out); err != nil {
		t.Fatalf("createProject: %v", err)
	}
	if got := strings.Join(strings.Fields(out.String()), ""); got != `{"name":"demo","creator":"user:kc/alice","public":true}` {
		t.Errorf("got %s", got)
	}
}
