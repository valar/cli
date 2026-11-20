package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/valar/cli/api"
)

// newScriptedAPI answers the nth request with the nth status, and with the
// last status once they run out. It records each request as "METHOD uri".
func newScriptedAPI(t *testing.T, statuses ...int) (*api.Client, *[]string) {
	t.Helper()
	var (
		mu       sync.Mutex
		requests []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Write([]byte(`{"version":"v2"}`))
			return
		}
		mu.Lock()
		defer mu.Unlock()
		status := statuses[min(len(requests), len(statuses)-1)]
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		w.WriteHeader(status)
		if status == http.StatusOK {
			w.Write([]byte(`{"status":"ok"}`))
		} else {
			fmt.Fprintf(w, `{"error":%q}`, http.StatusText(status))
		}
	}))
	t.Cleanup(srv.Close)
	client, err := api.NewClient(srv.URL, "token")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client, &requests
}

func fastDeleteRetries(t *testing.T) {
	t.Helper()
	prev := deleteRetryDelays
	deleteRetryDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { deleteRetryDelays = prev })
}

func terminalInput(s string) *confirmation {
	return &confirmation{in: strings.NewReader(s), terminal: true}
}

func TestDeleteService(t *testing.T) {
	const route = "DELETE /projects/demo/services/web"
	for _, tc := range []struct {
		Name         string
		Confirm      *confirmation
		Statuses     []int
		WantRequests int
		WantKind     errorKind
		WantErr      string
	}{
		{"Confirmed", terminalInput("web\n"), []int{200}, 1, 0, ""},
		{"Yes", nil, []int{200}, 1, 0, ""},
		{"Mismatch", terminalInput("api\n"), []int{200}, 0, kindInvalid, "aborted"},
		{"NotATerminal", &confirmation{in: strings.NewReader("web\n")}, []int{200}, 0, kindInvalid, "pass --yes"},
		{"NotFound", nil, []int{404}, 1, kindNotFound, "Not Found"},
		{"RetryThenNotFound", nil, []int{503, 404}, 2, 0, ""},
		{"ConflictRetried", nil, []int{409, 409, 200}, 3, 0, ""},
		{"ConflictPersists", nil, []int{409}, 4, kindConflict, "Conflict"},
		{"ServerErrorPersists", nil, []int{500}, 4, kindUnavailable, "Internal Server Error"},
		{"ForbiddenNotRetried", nil, []int{403}, 1, kindNotAllowed, "Forbidden"},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			fastDeleteRetries(t)
			client, requests := newScriptedAPI(t, tc.Statuses...)
			var out bytes.Buffer
			err := deleteService(client, "demo", "web", tc.Confirm, &out)
			if len(*requests) != tc.WantRequests {
				t.Errorf("requests %v, want %d", *requests, tc.WantRequests)
			}
			for _, r := range *requests {
				if r != route {
					t.Errorf("request %q, want %q", r, route)
				}
			}
			if tc.WantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.WantErr) {
					t.Fatalf("error = %v, want one containing %q", err, tc.WantErr)
				}
				if got := classify(err, kindFailed); got != tc.WantKind {
					t.Errorf("kind = %v, want %v", got, tc.WantKind)
				}
				return
			}
			if err != nil {
				t.Fatalf("deleteService: %v", err)
			}
			if !strings.Contains(out.String(), "Deleted service web.") {
				t.Errorf("output %q does not report the deletion", out.String())
			}
		})
	}
}

func TestDeleteService_PromptNamesService(t *testing.T) {
	client, _ := newScriptedAPI(t, 200)
	var out bytes.Buffer
	deleteService(client, "demo", "web", terminalInput("no\n"), &out)
	if !strings.Contains(out.String(), "Deleting service web in project demo") || !strings.Contains(out.String(), "Type the service name") {
		t.Errorf("prompt %q does not name the service", out.String())
	}
}

// runCLI runs the root command against endpoint with JSON output and returns
// what it printed on stdout.
func runCLI(t *testing.T, endpoint string, args ...string) string {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(cfgPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VALARCONFIG", cfgPath)
	t.Setenv("VALAR_ENDPOINT", endpoint)
	t.Setenv("VALAR_TOKEN", "token")
	out := setJSON(t, false)
	t.Cleanup(func() {
		serviceDeleteYes, projectsDeleteYes, projectsDeleteCascade = false, false, false
		rootCmd.SetArgs(nil)
	})
	rootCmd.SetArgs(append(args, "--json"))
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("valar %s: %v", strings.Join(args, " "), err)
	}
	return out.String()
}

func TestDeleteCommands_JSON(t *testing.T) {
	for _, tc := range []struct {
		Name        string
		Args        []string
		WantRequest string
		Want        string
	}{
		{"ServiceFromConfig", []string{"service", "delete", "--yes"}, "DELETE /projects/p/services/s", `{"service":"s","deleted":true}`},
		{"NamedService", []string{"service", "delete", "--yes", "api"}, "DELETE /projects/p/services/api", `{"service":"api","deleted":true}`},
		{"Project", []string{"project", "delete", "--yes", "demo"}, "DELETE /projects/demo", `{"project":"demo","deleted":true}`},
		{"ProjectCascade", []string{"project", "delete", "--yes", "--cascade", "demo"}, "DELETE /projects/demo?cascade=true", `{"project":"demo","deleted":true}`},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			writeServiceConfig(t, deployConfig)
			client, requests := newScriptedAPI(t, 200)
			out := runCLI(t, client.Endpoint, tc.Args...)
			if len(*requests) != 1 || (*requests)[0] != tc.WantRequest {
				t.Errorf("requests %v, want [%s]", *requests, tc.WantRequest)
			}
			if got := strings.Join(strings.Fields(out), ""); got != tc.Want {
				t.Errorf("printed %s, want %s", got, tc.Want)
			}
			var doc map[string]any
			if err := json.Unmarshal([]byte(out), &doc); err != nil {
				t.Errorf("output is not JSON: %v", err)
			}
		})
	}
}
