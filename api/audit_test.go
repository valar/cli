package api

import (
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestListAuditEvents(t *testing.T) {
	for _, tc := range []struct {
		Name      string
		Before    int64
		Limit     int
		WantQuery string
	}{
		{"FirstPage", 0, 0, ""},
		{"Limit", 0, 50, "limit=50"},
		{"NextPage", 1201, 100, "before=1201&limit=100"},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			var query string
			client, requests := newEnvTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				query = r.URL.RawQuery
				w.Write([]byte(`[{"id":1200,"at":"2026-11-21T10:00:00Z","kind":"result","attemptId":1199,"principal":"human::alice",` +
					`"tokenId":"` + ciID + `","client":"valar-cli/4.9.0","sourceIp":"192.0.2.1","service":"web",` +
					`"operation":"deploys.submit","target":"deployment=7","status":200,"count":0}]`))
			})
			events, err := client.ListAuditEvents("acme", tc.Before, tc.Limit)
			if err != nil {
				t.Fatalf("ListAuditEvents: %v", err)
			}
			if want := (recordedRequest{http.MethodGet, "/projects/acme/audit", ""}); (*requests)[0] != want {
				t.Errorf("request %+v, want %+v", (*requests)[0], want)
			}
			if query != tc.WantQuery {
				t.Errorf("query = %q, want %q", query, tc.WantQuery)
			}
			want := []AuditEvent{{
				ID: 1200, At: time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC), Kind: AuditResult, AttemptID: 1199,
				Principal: "human::alice", TokenID: ciID, Client: "valar-cli/4.9.0", SourceIP: "192.0.2.1", Service: "web",
				Operation: "deploys.submit", Target: "deployment=7", Status: 200,
			}}
			if !reflect.DeepEqual(events, want) {
				t.Errorf("events\n got %+v\nwant %+v", events, want)
			}
		})
	}
}

func TestListAuditEvents_Errors(t *testing.T) {
	for _, tc := range []struct {
		Name    string
		Status  int
		Body    string
		WantOld bool
		WantErr string
	}{
		{"UnknownRoute", http.StatusNotFound, "404 page not found\n", true, ErrNoAuditAPI.Error()},
		{"Forbidden", http.StatusForbidden, `{"error":"forbidden"}`, false, "Forbidden: forbidden"},
		{"Disabled", http.StatusServiceUnavailable, `{"error":"the audit log is disabled"}`, false, "Service Unavailable: the audit log is disabled"},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			client, _ := newEnvTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.Status)
				w.Write([]byte(tc.Body))
			})
			_, err := client.ListAuditEvents("acme", 0, 0)
			if got := errors.Is(err, ErrNoAuditAPI); got != tc.WantOld {
				t.Errorf("errors.Is(%v, ErrNoAuditAPI) = %v, want %v", err, got, tc.WantOld)
			}
			if err == nil || err.Error() != tc.WantErr {
				t.Errorf("error = %v, want %q", err, tc.WantErr)
			}
		})
	}
}
