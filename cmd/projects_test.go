package cmd

import (
	"bytes"
	"strings"
	"testing"
)

const deleteRoute = "DELETE /projects/demo"

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
		Name       string
		Project    string
		Confirm    *string
		WantDelete bool
		WantErr    string
	}{
		{"Confirmed", "demo", ptr("demo\n"), true, ""},
		{"ConfirmedWithoutNewline", "demo", ptr("  demo"), true, ""},
		{"Yes", "demo", nil, true, ""},
		{"Mismatch", "demo", ptr("dem\n"), false, "aborted"},
		{"NoInput", "demo", ptr(""), false, "--yes"},
		// The name is part of the request path, so it must be one.
		{"InvalidName", "demo/services/api", nil, false, "invalid project name"},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			client, requests := newFakeAPI(t, map[string]string{deleteRoute: `{"status":"ok"}`})
			var out bytes.Buffer
			var err error
			if tc.Confirm == nil {
				err = deleteProject(client, tc.Project, nil, &out)
			} else {
				err = deleteProject(client, tc.Project, strings.NewReader(*tc.Confirm), &out)
			}
			if deleted := len(*requests) == 1; deleted != tc.WantDelete {
				t.Fatalf("requests %+v, want delete %v", *requests, tc.WantDelete)
			}
			if tc.WantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.WantErr) {
					t.Fatalf("error = %v, want one containing %q", err, tc.WantErr)
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
	client, _ := newFakeAPI(t, map[string]string{deleteRoute: `{"status":"ok"}`})
	var out bytes.Buffer
	deleteProject(client, "demo", strings.NewReader("no\n"), &out)
	if !strings.Contains(out.String(), "retires its name") {
		t.Errorf("prompt %q does not warn that the name is retired", out.String())
	}
}

func ptr(s string) *string { return &s }
