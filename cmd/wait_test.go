package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/valar/cli/api"
)

// fakeRollout serves the routes waitForPush calls. Each poll takes the next
// build status and deployment list, repeating the last once exhausted.
type fakeRollout struct {
	mu          sync.Mutex
	buildStates []api.Build
	deployLists [][]api.Deployment
	services    []api.Service
	deployCalls int
	// serviceLog is the NDJSON body of the service log; logQuery records the
	// query it was read with.
	serviceLog string
	logQuery   string
}

func next[T any](items *[]T) T {
	item := (*items)[0]
	if len(*items) > 1 {
		*items = (*items)[1:]
	}
	return item
}

func (f *fakeRollout) client(t *testing.T) *api.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var body any
		switch path := r.URL.Path; {
		case path == "/":
			body = map[string]string{"version": "v2"}
		case path == "/projects/p/services/s/logs":
			f.logQuery = r.URL.RawQuery
			w.Write([]byte(f.serviceLog))
			return
		case strings.HasSuffix(path, "/logs"):
			return
		case strings.HasSuffix(path, "/inspect"):
			body = next(&f.buildStates)
		case strings.HasSuffix(path, "/deploys"):
			f.deployCalls++
			body = next(&f.deployLists)
		case strings.HasPrefix(path, "/projects/p/services/"):
			body = f.services
		default:
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	client, err := api.NewClient(srv.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestWaitForPush(t *testing.T) {
	defer func(d time.Duration) { pollInterval = d }(pollInterval)
	pollInterval = time.Millisecond

	building := api.Build{ID: "b1", Status: "building"}
	done := api.Build{ID: "b1", Status: "done"}
	deploy := func(status, msg string) []api.Deployment {
		return []api.Deployment{
			{Version: 2, Build: "b0", Status: "expired"},
			{Version: 3, Build: "b1", Status: status, Error: msg},
		}
	}
	for _, tc := range []struct {
		Name        string
		Fake        *fakeRollout
		Deploy      bool
		Timeout     time.Duration
		Error       string
		Output      string
		DeployCalls int
	}{
		{
			Name:   "build and deployment succeed",
			Fake:   &fakeRollout{buildStates: []api.Build{building, done}, deployLists: [][]api.Deployment{deploy("waiting", ""), deploy("scheduled", ""), deploy("done", "")}},
			Deploy: true,
			Output: "Deployed version 3.",
		},
		{
			Name:  "build fails",
			Fake:  &fakeRollout{buildStates: []api.Build{{ID: "b1", Status: "failed", Err: "exit status 1"}}},
			Error: "build b1 failed: exit status 1",
		},
		{
			Name:   "deployment fails",
			Fake:   &fakeRollout{buildStates: []api.Build{done}, deployLists: [][]api.Deployment{deploy("failed", "crashed on start")}},
			Deploy: true,
			Error:  "deployment of version 3 failed: crashed on start\nRead its log with: valar service logs --deployment 3",
		},
		{
			Name:   "deployment replaced by a newer one",
			Fake:   &fakeRollout{buildStates: []api.Build{done}, deployLists: [][]api.Deployment{deploy("expired", "")}},
			Deploy: true,
			Output: "replaced by a newer deployment",
		},
		{
			Name:   "without deploy only the build counts",
			Fake:   &fakeRollout{buildStates: []api.Build{done}},
			Output: "Build has succeeded.",
		},
		{
			Name:   "batch service has no deployment",
			Fake:   &fakeRollout{buildStates: []api.Build{done}, deployLists: [][]api.Deployment{nil}, services: []api.Service{{Name: "s", Kind: "batch"}}},
			Deploy: true,
			Output: "Batch services are not deployed",
		},
		{
			Name:   "function service without deployment",
			Fake:   &fakeRollout{buildStates: []api.Build{done}, deployLists: [][]api.Deployment{nil}, services: []api.Service{{Name: "s", Kind: "function"}}},
			Deploy: true,
			Error:  "no deployment was created for build b1",
		},
		{
			Name:    "build outlasts the timeout",
			Fake:    &fakeRollout{buildStates: []api.Build{building}},
			Timeout: -time.Second,
			Error:   "timed out waiting for build b1, which is building",
		},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			timeout := tc.Timeout
			if timeout == 0 {
				timeout = time.Minute
			}
			var out strings.Builder
			err := waitForPush(tc.Fake.client(t), "p", "s", "b1", tc.Deploy, timeout, &out)
			if tc.Error != "" {
				if err == nil || err.Error() != tc.Error {
					t.Fatalf("error = %v, want %q", err, tc.Error)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(out.String(), tc.Output) {
				t.Errorf("output %q lacks %q", out.String(), tc.Output)
			}
			if !tc.Deploy && tc.Fake.deployCalls != 0 {
				t.Errorf("listed deployments %d times without deploy", tc.Fake.deployCalls)
			}
		})
	}
}

func TestServiceURLs_CustomDomainsFirst(t *testing.T) {
	svc := api.Service{Name: "homepage", Domains: []string{
		"lennart-homepage.invoke.valar.dev",
		"espe.tech",
		"lennart-homepage.valar.app",
	}}
	want := []string{
		"https://espe.tech",
		"https://lennart-homepage.valar.app",
		"https://lennart-homepage.invoke.valar.dev",
	}
	if got := serviceURLs("lennart", svc); !reflect.DeepEqual(got, want) {
		t.Errorf("serviceURLs = %v, want %v", got, want)
	}
	if got := serviceURLs("lennart", api.Service{Name: "fresh"}); len(got) != 0 {
		t.Errorf("a service without domains has URLs %v", got)
	}
}

func TestEnableService_RetriesWhileStopping(t *testing.T) {
	defer func(d time.Duration) { pollInterval = d }(pollInterval)
	pollInterval = time.Millisecond
	var mu sync.Mutex
	conflicts := 2
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/":
			w.Write([]byte(`{"version":"v2"}`))
		case r.URL.Query().Get("disabled") != "false":
			t.Errorf("enable sent disabled=%q", r.URL.Query().Get("disabled"))
		case conflicts > 0:
			conflicts--
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"error":"service is still stopping, try again in a few seconds"}`))
		default:
			w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	t.Cleanup(srv.Close)
	client, err := api.NewClient(srv.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := enableService(client, "p", "s", &out); err != nil {
		t.Fatalf("enableService: %v", err)
	}
	if conflicts != 0 || !strings.Contains(out.String(), "Waiting") {
		t.Errorf("conflicts left %d, output %q", conflicts, out.String())
	}

	defer func(d time.Duration) { enableRetryWindow = d }(enableRetryWindow)
	enableRetryWindow = -time.Second
	conflicts = 1
	if err := enableService(client, "p", "s", &out); err == nil {
		t.Error("a conflict past the retry window was not reported")
	}
}

func TestWaitForPush_DeploymentFailure(t *testing.T) {
	defer func(d time.Duration) { pollInterval = d }(pollInterval)
	pollInterval = time.Millisecond
	defer func(f func() bool) { stderrIsTerminal = f }(stderrIsTerminal)

	const hint = "\nRead its log with: valar service logs --deployment 3"
	health := &api.DeploymentFailure{Kind: "health", Message: "not ready within 300s"}
	deploys := func(failure *api.DeploymentFailure) []api.Deployment {
		return []api.Deployment{
			{Version: 1, Build: "b0", Status: "expired"},
			{Version: 2, Build: "b0", Status: "done"},
			{Version: 3, Build: "b1", Status: "failed", Error: "not ready within 300s", Failure: failure},
		}
	}
	log := `{"stream":"stdout","line":"listening on :8080"}` + "\n" + `{"stream":"stderr","line":"\u001b[31mpanic: no database\u001b[0m"}` + "\n"
	for _, tc := range []struct {
		Name     string
		Failure  *api.DeploymentFailure
		CI       string
		Terminal bool
		Error    string
		LogShown bool
	}{
		{
			Name:    "failure kind and serving version",
			Failure: health,
			Error:   "deployment of version 3 failed (health): not ready within 300s; version 2 keeps serving" + hint,
		},
		{
			Name:  "server without failure kinds",
			Error: "deployment of version 3 failed: not ready within 300s" + hint,
		},
		{
			Name:     "terminal shows the end of the log",
			Failure:  health,
			Terminal: true,
			Error:    "deployment of version 3 failed (health): not ready within 300s; version 2 keeps serving" + hint,
			LogShown: true,
		},
		{
			Name:     "CI prints no log lines",
			Failure:  health,
			CI:       "true",
			Terminal: true,
			Error:    "deployment of version 3 failed (health): not ready within 300s; version 2 keeps serving" + hint,
		},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			t.Setenv("CI", tc.CI)
			stderrIsTerminal = func() bool { return tc.Terminal }
			fake := &fakeRollout{
				buildStates: []api.Build{{ID: "b1", Status: "done"}},
				deployLists: [][]api.Deployment{deploys(tc.Failure)},
				serviceLog:  log,
			}
			var out strings.Builder
			err := waitForPush(fake.client(t), "p", "s", "b1", true, time.Minute, &out)
			if err == nil || err.Error() != tc.Error {
				t.Fatalf("error = %v, want %q", err, tc.Error)
			}
			if kind := classify(err, kindUnavailable); kind != kindFailed {
				t.Errorf("kind = %v, want failed", kind)
			}
			shown := strings.Contains(out.String(), "Last lines of version 3:")
			if shown != tc.LogShown {
				t.Fatalf("log shown = %v, want %v; output %q", shown, tc.LogShown, out.String())
			}
			if !tc.LogShown {
				if fake.logQuery != "" {
					t.Errorf("read the service log with %q", fake.logQuery)
				}
				return
			}
			if want := "  \\x1b[31mpanic: no database\\x1b[0m\n"; !strings.HasSuffix(out.String(), want) {
				t.Errorf("output %q does not end with the escaped line %q", out.String(), want)
			}
			for _, param := range []string{"deployment=3", "seek=end", "skip=20"} {
				if !strings.Contains(fake.logQuery, param) {
					t.Errorf("log query %q lacks %s", fake.logQuery, param)
				}
			}
		})
	}
}
