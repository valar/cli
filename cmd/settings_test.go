package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/valar/cli/api"
)

const (
	artifactRoute = "POST /projects/p/services/s/artifacts"
	buildRoute    = "POST /projects/p/services/s/builds"
	settingsRoute = "PATCH /projects/p/services/s/settings"
	settingsPath  = "/projects/p/services/s/settings"
	settingsView  = `{"settings":{"cpu":500,"memory":512,"http":true,"minRunning":0},"explicit":["cpu","memory"],` +
		`"limits":{"cpu":{"min":100,"max":2000,"step":100},"memory":{"min":128,"max":4096},"minRunning":{"min":0,"max":16},` +
		`"budget":{"memory":{"used":1536,"allowed":51200},"cpu":{"used":1500,"allowed":50000}},"runningInstances":{"used":0,"allowed":0}}}`
)

const settingsConfig = `project: p
service: s
build:
  constructor: go
deployment:
  cpu: 500
  memory: 512
`

// routeOrder returns the "METHOD /path" of each request.
func routeOrder(requests []recordedRequest) []string {
	order := make([]string, len(requests))
	for i, r := range requests {
		order[i] = r.Method + " " + r.Path
	}
	return order
}

func TestSubmitPush_Settings(t *testing.T) {
	const noSettings = "project: p\nservice: s\nbuild:\n  constructor: go\ndeployment:\n  skip: false\n"
	for _, tc := range []struct {
		Name      string
		Config    string
		Deploy    bool
		Status    int
		Response  string
		WantPatch string
		WantKind  errorKind
		WantErr   []string
	}{
		{Name: "PatchBeforeBuild", Config: settingsConfig, Deploy: true, WantPatch: `{"cpu":500,"memory":512}`},
		{Name: "OnlyDeclared", Config: "project: p\nservice: s\nbuild:\n  constructor: go\ndeployment:\n  memory: 2048\n", Deploy: true, WantPatch: `{"memory":2048}`},
		{Name: "NothingDeclared", Config: noSettings, Deploy: true},
		{Name: "NoDeploymentSection", Config: "project: p\nservice: s\nbuild:\n  constructor: go\n", Deploy: true},
		{Name: "NoDeploy", Config: settingsConfig, Deploy: false},
		{
			Name: "Forbidden", Config: settingsConfig, Deploy: true, WantPatch: `{"cpu":500,"memory":512}`,
			Status: http.StatusForbidden, Response: `{"error":"raising memory needs a token with scope all","field":"memory"}`,
			WantKind: kindNotAllowed, WantErr: []string{"raising memory needs a token with scope all", "full access"},
		},
		{
			Name: "Budget", Config: settingsConfig, Deploy: true, WantPatch: `{"cpu":500,"memory":512}`,
			Status: http.StatusForbidden, Response: `{"error":"the project's services would request 52000 MiB of memory, above its budget of 51200 MiB","field":"memory"}`,
			WantKind: kindNotAllowed, WantErr: []string{"above its budget", "budget"},
		},
		{
			Name: "Invalid", Config: settingsConfig, Deploy: true, WantPatch: `{"cpu":500,"memory":512}`,
			Status: http.StatusBadRequest, Response: `{"error":"memory must be between 128 and 4096 MiB","field":"memory"}`,
			WantKind: kindInvalid, WantErr: []string{"memory must be between 128 and 4096 MiB"},
		},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			cfg, _ := writeServiceConfig(t, tc.Config)
			response := settingsView
			if tc.Response != "" {
				response = tc.Response
			}
			routes := map[string]string{
				envRoute:      `[]`,
				artifactRoute: `{"artifact":"art"}`,
				settingsRoute: response,
				buildRoute:    `{"id":"b1","status":"scheduled"}`,
			}
			statuses := map[string]int{}
			if tc.Status != 0 {
				statuses[settingsRoute] = tc.Status
			}
			client, requests := newFakeAPIWithStatus(t, routes, statuses)
			build, err := submitPush(client, cfg, io.NopCloser(strings.NewReader("archive")), "", "go", tc.Deploy)

			patches := requestsTo(*requests, http.MethodPatch, settingsPath)
			switch {
			case tc.WantPatch == "" && len(patches) != 0:
				t.Errorf("sent settings %+v, want none", patches)
			case tc.WantPatch != "" && (len(patches) != 1 || patches[0].Body != tc.WantPatch):
				t.Errorf("settings requests %+v, want one with body %s", patches, tc.WantPatch)
			}
			builds := requestsTo(*requests, http.MethodPost, "/projects/p/services/s/builds")
			if tc.WantErr != nil {
				if err == nil {
					t.Fatalf("pushed build %+v, want an error", build)
				}
				for _, want := range tc.WantErr {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q lacks %q", err, want)
					}
				}
				if got := classify(err, kindFailed); got != tc.WantKind {
					t.Errorf("kind = %v, want %v", got, tc.WantKind)
				}
				if len(builds) != 0 {
					t.Errorf("submitted a build after a refusal: %+v", builds)
				}
				return
			}
			if err != nil {
				t.Fatalf("submitPush: %v", err)
			}
			order := strings.Join(routeOrder(*requests), ", ")
			want := envRoute + ", " + artifactRoute + ", " + buildRoute
			if tc.WantPatch != "" {
				want = envRoute + ", " + artifactRoute + ", " + settingsRoute + ", " + buildRoute
			}
			if order != want {
				t.Errorf("requests %s, want %s", order, want)
			}
			var req api.BuildRequest
			if err := json.Unmarshal([]byte(builds[0].Body), &req); err != nil {
				t.Fatal(err)
			}
			if req.Deployment.Skip == tc.Deploy {
				t.Errorf("build request skip = %v, want %v", req.Deployment.Skip, !tc.Deploy)
			}
		})
	}
}

func TestSubmitPush_OldServer(t *testing.T) {
	cfg, _ := writeServiceConfig(t, settingsConfig)
	client, requests := newFakeAPI(t, map[string]string{
		envRoute:      `[]`,
		artifactRoute: `{"artifact":"art"}`,
		buildRoute:    `{"id":"b1"}`,
	})
	_, err := submitPush(client, cfg, io.NopCloser(strings.NewReader("archive")), "", "go", true)
	if !errors.Is(err, api.ErrNoSettingsAPI) {
		t.Fatalf("got %v, want ErrNoSettingsAPI", err)
	}
	if builds := requestsTo(*requests, http.MethodPost, "/projects/p/services/s/builds"); len(builds) != 0 {
		t.Errorf("submitted a build without its settings: %+v", builds)
	}
}

func TestSkipsDeploy(t *testing.T) {
	for _, tc := range []struct {
		Name   string
		Config string
		Want   bool
	}{
		{"Skip", "project: p\nservice: s\ndeployment:\n  skip: true\n", true},
		{"NoSkip", "project: p\nservice: s\ndeployment:\n  skip: false\n", false},
		{"NoSection", "project: p\nservice: s\n", false},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			cfg, _ := writeServiceConfig(t, tc.Config)
			if got := skipsDeploy(cfg); got != tc.Want {
				t.Errorf("skipsDeploy = %v, want %v", got, tc.Want)
			}
		})
	}
}

func TestParseSettingAssignments(t *testing.T) {
	for _, tc := range []struct {
		Name    string
		Args    []string
		Want    string
		WantErr string
	}{
		{"CPUAndMemory", []string{"cpu=500", "memory=2048"}, `{"cpu":500,"memory":2048}`, ""},
		{"HTTPAndMinRunning", []string{"http=false", "minRunning=1"}, `{"http":false,"minRunning":1}`, ""},
		{"Unknown", []string{"disk=10"}, "", `unknown setting "disk"`},
		{"NotInteger", []string{"cpu=0.5"}, "", "cpu must be an integer"},
		{"NotBool", []string{"http=maybe"}, "", "http must be true or false"},
		{"NoValue", []string{"cpu"}, "", "not name=value"},
		{"Twice", []string{"cpu=500", "cpu=600"}, "", "given twice"},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			patch, err := parseSettingAssignments(tc.Args)
			if tc.WantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.WantErr) {
					t.Fatalf("got %v, want an error containing %q", err, tc.WantErr)
				}
				if classify(err, kindFailed) != kindInvalid {
					t.Errorf("kind = %v, want invalid", classify(err, kindFailed))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, _ := json.Marshal(patch)
			if string(got) != tc.Want {
				t.Errorf("patch = %s, want %s", got, tc.Want)
			}
		})
	}
}

func TestPrintSettings(t *testing.T) {
	// A field the CLI does not know yet passes through with --json.
	const response = `{"settings":{"cpu":500,"memory":512,"http":true,"minRunning":0},"explicit":["cpu"],"limits":{"cpu":{"min":100,"max":2000,"step":100},"memory":{"min":128,"max":4096},"minRunning":{"min":0,"max":16}},"future":1}`
	client, requests := newFakeAPI(t, map[string]string{
		"GET " + settingsPath: response,
		settingsRoute:         response,
	})
	view, err := client.ServiceSettings("p", "s")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Human", func(t *testing.T) {
		out := setJSON(t, false)
		if err := printSettings(stdout, view); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(out.String(), "\n")
		for i, want := range map[int][]string{
			1: {"cpu", "500", "set", "100-2000 millicores, steps of 100"},
			2: {"memory", "512", "default", "128-4096 MiB"},
			3: {"http", "true", "default"},
			4: {"minRunning", "0", "default", "0-16"},
		} {
			for _, field := range want {
				if !strings.Contains(lines[i], field) {
					t.Errorf("line %q lacks %q", lines[i], field)
				}
			}
		}
		if strings.Contains(out.String(), "budget") {
			t.Errorf("printed a budget the server did not send:\n%s", out)
		}
	})

	t.Run("HumanBudget", func(t *testing.T) {
		var budgeted api.SettingsView
		if err := json.Unmarshal([]byte(settingsView), &budgeted); err != nil {
			t.Fatal(err)
		}
		out := setJSON(t, false)
		if err := printSettings(stdout, &budgeted); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Project budget: 1536 of 51200 MiB memory, 1500 of 50000 millicores CPU.", "Instances that always run: 0 of 0."} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("output lacks %q:\n%s", want, out)
			}
		}
	})

	t.Run("JSON", func(t *testing.T) {
		out := setJSON(t, true)
		if err := printSettings(stdout, view); err != nil {
			t.Fatal(err)
		}
		var got, want bytes.Buffer
		json.Compact(&got, out.Bytes())
		json.Compact(&want, []byte(response))
		if got.String() != want.String() {
			t.Errorf("printed %s, want the response %s", got.String(), want.String())
		}
	})

	t.Run("Reset", func(t *testing.T) {
		if _, err := client.PatchSettings("p", "s", api.SettingsPatch{"memory": nil}); err != nil {
			t.Fatal(err)
		}
		patches := requestsTo(*requests, http.MethodPatch, settingsPath)
		if len(patches) != 1 || patches[0].Body != `{"memory":null}` {
			t.Errorf("settings requests %+v, want one resetting memory", patches)
		}
	})
}

func TestSettingsRefusal_HintOnlyForSettings(t *testing.T) {
	for _, tc := range []struct {
		Name     string
		Status   int
		Server   api.JSONError
		WantHint bool
	}{
		{"Scope", http.StatusForbidden, api.JSONError{Err: "raising cpu needs a token with scope all", Field: "cpu"}, true},
		{"PlainForbidden", http.StatusForbidden, api.JSONError{Err: "forbidden"}, false},
		{"Invalid", http.StatusBadRequest, api.JSONError{Err: "cpu must be between", Field: "cpu"}, false},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			err := settingsRefusal(api.Error{StatusCode: tc.Status, ServerError: tc.Server})
			if got := strings.Contains(err.Error(), "full access"); got != tc.WantHint {
				t.Errorf("hint in %q = %v, want %v", err, got, tc.WantHint)
			}
			if classify(err, kindFailed) != statusKind(tc.Status) {
				t.Errorf("kind = %v, want %v", classify(err, kindFailed), statusKind(tc.Status))
			}
		})
	}
}
