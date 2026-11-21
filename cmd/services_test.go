package cmd

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/valar/cli/api"
)

func TestServiceKind(t *testing.T) {
	for _, tc := range []struct{ In, Want string }{
		{"function", "function"},
		{"batch", "batch"},
		// A server predating the field must not be reported as "function":
		// that would present a guess as fact.
		{"", "-"},
		{"something-new", "something-new"},
	} {
		if got := serviceKind(tc.In); got != tc.Want {
			t.Errorf("serviceKind(%q) = %q, want %q", tc.In, got, tc.Want)
		}
	}
}

// Humanising the zero timestamp yields "a long while ago", which reads as
// deployed in the distant past. A batch service is never deployed at all.
func TestDeployedColumns_NeverDeployed(t *testing.T) {
	svc := api.Service{Name: "batchdemo", Kind: "batch"}
	if got := lastDeployed(svc); got != "-" {
		t.Errorf("lastDeployed = %q, want -", got)
	}
	if got := deployedVersion(svc); got != "-" {
		t.Errorf("deployedVersion = %q, want -", got)
	}
}

func TestDeployedColumns_Deployed(t *testing.T) {
	svc := api.Service{Name: "web", Kind: "function", Deployment: 7, DeployedAt: time.Now().Add(-time.Hour)}
	if got := deployedVersion(svc); got != "7" {
		t.Errorf("deployedVersion = %q, want 7", got)
	}
	if got := lastDeployed(svc); got == "-" {
		t.Error("lastDeployed should render a time for a deployed service")
	}
}

func TestServiceIsBatch(t *testing.T) {
	if !(api.Service{Kind: "batch"}).IsBatch() {
		t.Error("batch service should report IsBatch")
	}
	for _, kind := range []string{"function", ""} {
		if (api.Service{Kind: kind}).IsBatch() {
			t.Errorf("kind %q should not report IsBatch", kind)
		}
	}
}

const logsPath = "/projects/p/services/s/logs"

func TestShowServiceLogs(t *testing.T) {
	const (
		record   = `{"ts":"2026-11-21T10:00:00.000000001Z","deployment":"3","stream":"output","line":"hello","instance":"ab12","future":1}`
		platform = `{"ts":"2026-11-21T10:00:01Z","deployment":"3","stream":"platform","line":"instance started","event":"instance.start","attrs":{"cpu":"shared"}}`
	)
	for _, tc := range []struct {
		Name        string
		Options     api.LogOptions
		JSON        bool
		Timestamps  bool
		ContentType string
		Body        string
		WantQuery   url.Values
		WantOutput  string
		WantErr     bool
	}{
		{
			Name:        "RecordsPassedThrough",
			JSON:        true,
			ContentType: "application/x-ndjson",
			Body:        record + "\n" + platform + "\n",
			WantQuery:   url.Values{"format": {"json"}, "seek": {"start"}, "skip": {"0"}},
			WantOutput:  record + "\n" + platform + "\n",
		},
		{
			Name:        "KeepaliveSkipped",
			JSON:        true,
			ContentType: "application/x-ndjson",
			Body:        `{"keepalive":true}` + "\n" + record + "\n" + `{"keepalive":true}` + "\n",
			WantQuery:   url.Values{"format": {"json"}, "seek": {"start"}, "skip": {"0"}},
			WantOutput:  record + "\n",
		},
		{
			Name:        "PlainTextFallback",
			JSON:        true,
			ContentType: "text/plain; charset=utf-8",
			Body:        "\n{\"msg\":\"json, but no line\"}\n[platform] instance started\nlast",
			WantQuery:   url.Values{"format": {"json"}, "seek": {"start"}, "skip": {"0"}},
			WantOutput:  `{"line":""}` + "\n" + `{"line":"{\"msg\":\"json, but no line\"}"}` + "\n" + `{"line":"[platform] instance started"}` + "\n" + `{"line":"last"}` + "\n",
		},
		{
			Name:        "FramedTextFallback",
			JSON:        true,
			ContentType: "application/x-ndjson",
			Body:        `{"keepalive":true}` + "\n" + `{"data":"aGVsbG8Kd29y"}` + "\n" + `{"data":"bGQK"}` + "\n",
			WantQuery:   url.Values{"format": {"json"}, "seek": {"start"}, "skip": {"0"}},
			WantOutput:  `{"line":"hello"}` + "\n" + `{"line":"world"}` + "\n",
		},
		{
			Name:        "All",
			Options:     api.LogOptions{All: true},
			JSON:        true,
			ContentType: "application/x-ndjson",
			Body:        record + "\n",
			WantQuery:   url.Values{"format": {"json"}, "all": {"true"}},
			WantOutput:  record + "\n",
		},
		{
			Name:        "Deployment",
			Options:     api.LogOptions{Deployment: 3},
			JSON:        true,
			ContentType: "application/x-ndjson",
			Body:        record + "\n",
			WantQuery:   url.Values{"format": {"json"}, "deployment": {"3"}, "seek": {"start"}, "skip": {"0"}},
			WantOutput:  record + "\n",
		},
		{
			Name:        "HumanUnchanged",
			Options:     api.LogOptions{All: true},
			ContentType: "text/plain; charset=utf-8",
			Body:        "hello\n[platform] instance started\n",
			WantQuery:   url.Values{"all": {"true"}},
			WantOutput:  "hello\n[platform] instance started\n",
		},
		{
			Name:        "Timestamps",
			Options:     api.LogOptions{Tail: true},
			Timestamps:  true,
			ContentType: "application/x-ndjson",
			Body:        record + "\n" + platform + "\n",
			WantQuery:   url.Values{"format": {"json"}, "seek": {"end"}, "skip": {"0"}},
			WantOutput:  "2026-11-21T10:00:00.000000001Z hello\n2026-11-21T10:00:01Z [platform] instance started\n",
		},
		{
			Name:    "AllWithDeployment",
			Options: api.LogOptions{All: true, Deployment: 3},
			WantErr: true,
		},
		{
			Name:    "AllWithSkip",
			Options: api.LogOptions{All: true, Skip: 5},
			WantErr: true,
		},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			cfg, _ := writeServiceConfig(t, deployConfig)
			var queries []url.Values
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/" {
					w.Write([]byte(`{"version":"v2"}`))
					return
				}
				if r.URL.Path != logsPath {
					http.NotFound(w, r)
					return
				}
				queries = append(queries, r.URL.Query())
				w.Header().Set("Content-Type", tc.ContentType)
				w.Write([]byte(tc.Body))
			}))
			t.Cleanup(srv.Close)
			client, err := api.NewClient(srv.URL, "token")
			if err != nil {
				t.Fatalf("new client: %v", err)
			}
			out := setJSON(t, tc.JSON)

			err = showServiceLogs(client, cfg, tc.Options, tc.Timestamps)
			if tc.WantErr {
				if classify(err, kindFailed) != kindInvalid || len(queries) != 0 {
					t.Errorf("got %v after %d requests, want an invalid error before any", err, len(queries))
				}
				return
			}
			if err != nil {
				t.Fatalf("showServiceLogs: %v", err)
			}
			if len(queries) != 1 || !reflect.DeepEqual(queries[0], tc.WantQuery) {
				t.Errorf("got queries %v, want %v", queries, tc.WantQuery)
			}
			if out.String() != tc.WantOutput {
				t.Errorf("got\n%s\nwant\n%s", out.String(), tc.WantOutput)
			}
		})
	}
}
