package cmd

import (
	"bytes"
	"strings"
	"testing"
)

const (
	deleteRoute  = "DELETE /projects/demo"
	cascadeRoute = "DELETE /projects/demo?cascade=true"
)

func TestCreateProject(t *testing.T) {
	for _, tc := range []struct {
		Name       string
		Public     bool
		Response   string
		WantBody   string
		WantAccess string
	}{
		{"Private", false, `{"name":"demo","creator":"user:kc/alice","public":false}`, `{"name":"demo","public":false}`, "private"},
		{"Public", true, `{"name":"demo","creator":"user:kc/alice","public":true}`, `{"name":"demo","public":true}`, "public, anyone can invoke"},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			client, requests := newFakeAPI(t, map[string]string{"POST /projects": tc.Response})
			var out bytes.Buffer
			if err := createProject(client, "demo", tc.Public, &out); err != nil {
				t.Fatalf("createProject: %v", err)
			}
			if got := (*requests)[0].Body; got != tc.WantBody {
				t.Errorf("body = %s, want %s", got, tc.WantBody)
			}
			for _, want := range []string{"demo", "user:kc/alice", tc.WantAccess} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output %q lacks %q", out.String(), want)
				}
			}
		})
	}
}

func TestDeleteProject(t *testing.T) {
	for _, tc := range []struct {
		Name         string
		Project      string
		Cascade      bool
		Confirm      *confirmation
		Statuses     []int
		WantRequests []string
		WantKind     errorKind
		WantErr      string
	}{
		{"Confirmed", "demo", false, terminalInput("demo\n"), []int{200}, []string{deleteRoute}, 0, ""},
		{"ConfirmedWithoutNewline", "demo", false, terminalInput("  demo"), []int{200}, []string{deleteRoute}, 0, ""},
		{"Yes", "demo", false, nil, []int{200}, []string{deleteRoute}, 0, ""},
		{"Mismatch", "demo", false, terminalInput("dem\n"), []int{200}, nil, kindInvalid, "aborted"},
		{"NoInput", "demo", false, terminalInput(""), []int{200}, nil, kindInvalid, "--yes"},
		// The name is part of the request path, so it must be one.
		{"InvalidName", "demo/services/api", false, nil, []int{200}, nil, kindInvalid, "invalid project name"},
		// Without cascade a conflict means the project isn't empty.
		{"NotEmptyNotRetried", "demo", false, nil, []int{409}, []string{deleteRoute}, kindConflict, "Conflict"},
		{"Cascade", "demo", true, terminalInput("demo\n"), []int{200}, []string{cascadeRoute}, 0, ""},
		{"CascadeNotATerminal", "demo", true, &confirmation{in: strings.NewReader("demo\n")}, []int{200}, nil, kindInvalid, "pass --yes"},
		{"CascadeConflictRetried", "demo", true, nil, []int{409, 200}, []string{cascadeRoute, cascadeRoute}, 0, ""},
		{"CascadeRetryThenNotFound", "demo", true, nil, []int{502, 404}, []string{cascadeRoute, cascadeRoute}, 0, ""},
		{"CascadeNotFound", "demo", true, nil, []int{404}, []string{cascadeRoute}, kindNotFound, "Not Found"},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			fastDeleteRetries(t)
			client, requests := newScriptedAPI(t, tc.Statuses...)
			var out bytes.Buffer
			err := deleteProject(client, tc.Project, tc.Cascade, tc.Confirm, &out)
			if strings.Join(*requests, ",") != strings.Join(tc.WantRequests, ",") {
				t.Fatalf("requests %v, want %v", *requests, tc.WantRequests)
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
				t.Fatalf("deleteProject: %v", err)
			}
			if !strings.Contains(out.String(), "can't be used again") {
				t.Errorf("output %q does not say the name is retired", out.String())
			}
		})
	}
}

func TestDeleteProject_PromptNamesRetirement(t *testing.T) {
	for _, tc := range []struct {
		Name    string
		Cascade bool
		Want    string
	}{
		{"Empty", false, "retires its name"},
		{"Cascade", true, "deletes all its services and domain claims"},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			client, _ := newScriptedAPI(t, 200)
			var out bytes.Buffer
			deleteProject(client, "demo", tc.Cascade, terminalInput("no\n"), &out)
			if !strings.Contains(out.String(), tc.Want) {
				t.Errorf("prompt %q lacks %q", out.String(), tc.Want)
			}
		})
	}
}

func ptr(s string) *string { return &s }
