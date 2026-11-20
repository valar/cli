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
			Error:  "deployment of version 3 failed: crashed on start",
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
