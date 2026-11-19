package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordedRequest struct {
	Method, Path, Body string
}

// newEnvTestServer answers with handler and records every request.
func newEnvTestServer(t *testing.T, handler http.HandlerFunc) (*Client, *[]recordedRequest) {
	t.Helper()
	var requests []recordedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests = append(requests, recordedRequest{r.Method, r.URL.Path, string(body)})
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return &Client{Endpoint: srv.URL, http: &http.Client{}}, &requests
}

func TestEnvRequests(t *testing.T) {
	client, requests := newEnvTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /projects/p/services/s/env":
			w.Write([]byte(`[{"name":"A","target":"runtime","secret":false,"value":"1","updatedBy":"u"},{"name":"S","target":"build","secret":true}]`))
		case "PATCH /projects/p/services/s/env":
			w.Write([]byte(`{"skipped":["B"]}`))
		case "GET /projects/p/services/s/env/changes":
			w.Write([]byte(`[{"id":2,"name":"A","target":"runtime","action":"set","origin":"api","secret":false,"createdBy":"u"}]`))
		default:
			http.NotFound(w, r)
		}
	})

	vars, err := client.ListEnv("p", "s")
	if err != nil {
		t.Fatalf("ListEnv: %v", err)
	}
	if len(vars) != 2 || vars[0].Value != "1" || !vars[1].Secret || vars[1].Target != EnvTargetBuild {
		t.Errorf("ListEnv decoded %+v", vars)
	}

	result, err := client.PatchEnv("p", "s", &EnvChangeset{
		Set:   map[string]EnvValue{"A": {Value: "1", Secret: true, Targets: []string{EnvTargetRuntime}}},
		Unset: map[string][]string{"OLD": {EnvTargetRuntime, EnvTargetBuild}},
	})
	if err != nil {
		t.Fatalf("PatchEnv: %v", err)
	}
	if len(result.Skipped) != 1 || result.Skipped[0] != "B" {
		t.Errorf("skipped = %v, want [B]", result.Skipped)
	}

	changes, err := client.ListEnvChanges("p", "s")
	if err != nil {
		t.Fatalf("ListEnvChanges: %v", err)
	}
	if len(changes) != 1 || changes[0].Action != "set" || changes[0].Origin != "api" {
		t.Errorf("ListEnvChanges decoded %+v", changes)
	}

	want := `{"set":{"A":{"value":"1","secret":true,"targets":["runtime"]}},"unset":{"OLD":["runtime","build"]}}`
	if got := (*requests)[1].Body; got != want {
		t.Errorf("PATCH body\n got %s\nwant %s", got, want)
	}
}

func TestEnvChangeset_OnlySeeded(t *testing.T) {
	got, _ := json.Marshal(EnvChangeset{Set: map[string]EnvValue{"A": {Value: "1", Targets: []string{EnvTargetBuild}}}, OnlySeeded: true})
	want := `{"set":{"A":{"value":"1","secret":false,"targets":["build"]}},"onlySeeded":true}`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

// The router answers unknown routes in plain text; the API answers in JSON.
// Only the former means the server predates the environment API.
func TestEnvError_MissingRoute(t *testing.T) {
	for _, tc := range []struct {
		Name    string
		Status  int
		Body    string
		WantOld bool
	}{
		{"UnknownRoute", http.StatusNotFound, "404 page not found\n", true},
		{"UnknownMethod", http.StatusMethodNotAllowed, "", true},
		{"UnknownService", http.StatusNotFound, `{"error":"service not found"}`, false},
		{"Forbidden", http.StatusForbidden, `{"error":"forbidden"}`, false},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			client, _ := newEnvTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.Status)
				w.Write([]byte(tc.Body))
			})
			_, err := client.ListEnv("p", "s")
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := errors.Is(err, ErrNoEnvAPI); got != tc.WantOld {
				t.Errorf("errors.Is(%v, ErrNoEnvAPI) = %v, want %v", err, got, tc.WantOld)
			}
		})
	}
}

func TestErrNoEnvAPI_NamesPlatformVersion(t *testing.T) {
	if !strings.Contains(ErrNoEnvAPI.Error(), EnvMinPlatform) {
		t.Errorf("%q does not name platform %s", ErrNoEnvAPI, EnvMinPlatform)
	}
}

func TestJSONError_Names(t *testing.T) {
	client, _ := newEnvTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		w.Write([]byte(`{"error":"value too large","names":["A","B"]}`))
	})
	_, err := client.PatchEnv("p", "s", &EnvChangeset{})
	if err == nil || !strings.Contains(err.Error(), "value too large: A, B") {
		t.Errorf("got %v, want the message and the names", err)
	}
}

func TestHasEnvAPI(t *testing.T) {
	for _, tc := range []struct {
		Name   string
		Status int
		Body   string
		Want   bool
	}{
		{"Available", http.StatusOK, `[]`, true},
		{"OldServer", http.StatusNotFound, "404 page not found\n", false},
		{"Disabled", http.StatusServiceUnavailable, `{"error":"environment API disabled"}`, false},
		// A new service has no env yet, but the server has the API.
		{"UnknownService", http.StatusNotFound, `{"error":"service not found"}`, true},
		{"NoReadPermission", http.StatusForbidden, `{"error":"forbidden"}`, true},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			client, _ := newEnvTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.Status)
				w.Write([]byte(tc.Body))
			})
			got, err := client.HasEnvAPI("p", "s")
			if err != nil {
				t.Fatalf("HasEnvAPI: %v", err)
			}
			if got != tc.Want {
				t.Errorf("HasEnvAPI = %v, want %v", got, tc.Want)
			}
		})
	}
}

func TestHasEnvAPI_Unreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	client := &Client{Endpoint: srv.URL, http: &http.Client{}}
	if _, err := client.HasEnvAPI("p", "s"); err == nil {
		t.Error("an unreachable server must be an error, not a guess")
	}
}

// Servers with the environment API treat a present environment key as an
// inline list, so requests must leave it out entirely.
func TestRequests_OmitEnvironment(t *testing.T) {
	var build BuildRequest
	build.Artifact = "art"
	build.Build.Constructor = "go"
	for _, tc := range []struct {
		Name string
		Req  interface{}
		Want string
	}{
		{"Build", build, `{"artifact":"art","build":{"constructor":"go"},"deployment":{"skip":false}}`},
		{"Deploy", DeployRequest{Build: "b1"}, `{"build":"b1"}`},
		{"Redeploy", DeployRequest{}, `{}`},
		{"Rollback", RollbackRequest{Version: 3, Environment: RollbackEnvCurrent}, `{"version":3,"environment":"current"}`},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			got, err := json.Marshal(tc.Req)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.Want {
				t.Errorf("got %s, want %s", got, tc.Want)
			}
		})
	}
}

func TestRollbackDeploy_DecodesChanged(t *testing.T) {
	client, requests := newEnvTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version":12,"status":"waiting","build":"b1","changed":["A","B"]}`))
	})
	result, err := client.RollbackDeploy("p", "s", &RollbackRequest{Version: 10, Environment: RollbackEnvPinned})
	if err != nil {
		t.Fatalf("RollbackDeploy: %v", err)
	}
	if result.Version != 12 || result.Build != "b1" || strings.Join(result.Changed, ",") != "A,B" {
		t.Errorf("decoded %+v", result)
	}
	if got := (*requests)[0]; got.Path != "/projects/p/services/s/deploys/rollback" || got.Body != `{"version":10,"environment":"pinned"}` {
		t.Errorf("request %+v", got)
	}
}
