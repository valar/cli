package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/valar/cli/api"
)

const deployConfig = `project: p
service: s
build:
    constructor: go
    environment:
        - BUILD_FLAG=1
deployment:
    environment:
        - A=1
`

const (
	envRoute     = "GET /projects/p/services/s/env"
	deployRoute  = "POST /projects/p/services/s/deploys"
	listRoute    = "GET /projects/p/services/s/deploys"
	rollbackPath = "/projects/p/services/s/deploys/rollback"
)

func requestsTo(requests []recordedRequest, method, path string) []recordedRequest {
	var matched []recordedRequest
	for _, r := range requests {
		if r.Method == method && r.Path == path {
			matched = append(matched, r)
		}
	}
	return matched
}

func TestDeployBuild(t *testing.T) {
	for _, tc := range []struct {
		Name     string
		EnvAPI   bool
		Build    string
		WantBody string
		WantErr  string
	}{
		{"Build", true, "b1", `{"build":"b1"}`, ""},
		{"Redeploy", true, "", `{}`, ""},
		// A server without the API still needs the inline list.
		{"OldServerBuild", false, "b1", `{"build":"b1","environment":[{"key":"A","value":"1","secret":false}]}`, ""},
		{"OldServerRedeploy", false, "", "", api.EnvMinPlatform},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			cfg, _ := writeServiceConfig(t, deployConfig)
			routes := map[string]string{deployRoute: `{"version":7}`}
			if tc.EnvAPI {
				routes[envRoute] = `[]`
			}
			client, requests := newFakeAPI(t, routes)
			var out bytes.Buffer
			err := deployBuild(client, cfg, tc.Build, &out)
			deploys := requestsTo(*requests, http.MethodPost, "/projects/p/services/s/deploys")
			if tc.WantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.WantErr) {
					t.Fatalf("got %v, want an error naming %s", err, tc.WantErr)
				}
				if len(deploys) != 0 {
					t.Errorf("deployed anyway: %+v", deploys)
				}
				return
			}
			if err != nil {
				t.Fatalf("deployBuild: %v", err)
			}
			if len(deploys) != 1 || deploys[0].Body != tc.WantBody {
				t.Errorf("deploy requests %+v, want one with body %s", deploys, tc.WantBody)
			}
			if out.String() != "7\n" {
				t.Errorf("printed %q, want the version", out.String())
			}
		})
	}
}

func TestNewBuildRequest(t *testing.T) {
	cfg, _ := writeServiceConfig(t, deployConfig)
	for _, tc := range []struct {
		Name   string
		Inline bool
		Want   string
	}{
		{"EnvAPI", false, `{"artifact":"art","kind":"batch","build":{"constructor":"go"},"deployment":{"skip":true}}`},
		{"OldServer", true, `{"artifact":"art","kind":"batch","build":{"constructor":"go","environment":[{"key":"BUILD_FLAG","value":"1","secret":false}]},"deployment":{"skip":true,"environment":[{"key":"A","value":"1","secret":false}]}}`},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			got, err := json.Marshal(newBuildRequest(cfg, "art", "batch", true, tc.Inline))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.Want {
				t.Errorf("got %s\nwant %s", got, tc.Want)
			}
		})
	}
}

func TestRollback(t *testing.T) {
	for _, tc := range []struct {
		Name     string
		EnvAPI   bool
		Env      string
		WantBody string
		WantErr  string
	}{
		{"Pinned", true, "pinned", `{"version":2,"environment":"pinned"}`, ""},
		{"Current", true, "current", `{"version":2,"environment":"current"}`, ""},
		// An old server ignores the field and rolls back pinned, as it always has.
		{"PinnedOldServer", false, "pinned", `{"version":2,"environment":"pinned"}`, ""},
		{"CurrentOldServer", false, "current", "", api.EnvMinPlatform},
		{"Unknown", true, "latest", "", "unknown --env"},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			cfg, _ := writeServiceConfig(t, deployConfig)
			routes := map[string]string{
				listRoute:              `[{"version":1},{"version":3},{"version":2}]`,
				"POST " + rollbackPath: `{"version":4,"changed":["A"]}`,
			}
			if tc.EnvAPI {
				routes[envRoute] = `[]`
			}
			client, requests := newFakeAPI(t, routes)
			var out, notes bytes.Buffer
			err := rollbackLatestDeployment(client, cfg, 1, tc.Env, &out, &notes)
			rollbacks := requestsTo(*requests, http.MethodPost, rollbackPath)
			if tc.WantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.WantErr) {
					t.Fatalf("got %v, want an error containing %q", err, tc.WantErr)
				}
				if len(rollbacks) != 0 {
					t.Errorf("rolled back anyway: %+v", rollbacks)
				}
				return
			}
			if err != nil {
				t.Fatalf("rollback: %v", err)
			}
			if len(rollbacks) != 1 || rollbacks[0].Body != tc.WantBody {
				t.Errorf("rollback requests %+v, want one with body %s", rollbacks, tc.WantBody)
			}
			if out.String() != "4\n" {
				t.Errorf("printed %q, want only the new version", out.String())
			}
			if !strings.Contains(notes.String(), "version 2 differs from the current one in A") {
				t.Errorf("notes %q do not name the changed variable", notes.String())
			}
		})
	}
}

func TestListDeployments_Failure(t *testing.T) {
	const deploys = `[
		{"version":5,"status":"failed","error":"crashed on start","build":"b5","createdAt":"2026-11-20T10:00:00Z"},
		{"version":6,"status":"done","error":"","build":"b6","createdAt":"2026-11-20T11:00:00Z"},
		{"version":7,"status":"failed","error":"not ready within 300s","build":"b7","createdAt":"2026-11-20T12:00:00Z",
		 "failure":{"kind":"health","message":"not ready within 300s"}}
	]`
	for _, tc := range []struct {
		Name string
		JSON bool
	}{
		{"Human", false},
		{"JSON", true},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			cfg, _ := writeServiceConfig(t, deployConfig)
			client, _ := newFakeAPI(t, map[string]string{listRoute: deploys})
			out := setJSON(t, tc.JSON)
			if err := listDeployments(client, cfg); err != nil {
				t.Fatalf("listDeployments: %v", err)
			}
			if !tc.JSON {
				lines := strings.Split(out.String(), "\n")
				for i, want := range map[int][]string{
					1: {"5", "failed ", "crashed on start"},
					3: {"7", "failed (health)", "not ready within 300s"},
				} {
					for _, field := range want {
						if !strings.Contains(lines[i], field) {
							t.Errorf("line %q lacks %q", lines[i], field)
						}
					}
				}
				if strings.Contains(lines[1], "(") {
					t.Errorf("a deployment without failure shows a kind: %q", lines[1])
				}
				return
			}
			var got []map[string]json.RawMessage
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatalf("decode %q: %v", out.String(), err)
			}
			if _, ok := got[0]["failure"]; ok {
				t.Errorf("a deployment without failure has one: %s", got[0]["failure"])
			}
			var failure bytes.Buffer
			json.Compact(&failure, got[2]["failure"])
			if want := `{"kind":"health","message":"not ready within 300s"}`; failure.String() != want {
				t.Errorf("failure = %s, want %s", failure.String(), want)
			}
		})
	}
}
