package api

import (
	"errors"
	"net/http"
	"testing"
)

func TestCreateProject(t *testing.T) {
	client, requests := newEnvTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"name":"demo","creator":"user:kc/alice","public":true}`))
	})
	project, err := client.CreateProject("demo", true)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if *project != (Project{Name: "demo", Creator: "user:kc/alice", Public: true}) {
		t.Errorf("decoded %+v", project)
	}
	want := recordedRequest{http.MethodPost, "/projects", `{"name":"demo","public":true}`}
	if got := (*requests)[0]; got != want {
		t.Errorf("request\n got %+v\nwant %+v", got, want)
	}
}

func TestCreateProject_SurfacesServerError(t *testing.T) {
	client, _ := newEnvTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":"project name is taken"}`))
	})
	_, err := client.CreateProject("demo", false)
	var apiErr Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusConflict {
		t.Fatalf("got %v, want a 409 API error", err)
	}
	if got, want := err.Error(), "Conflict: project name is taken"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}

func TestDeleteProject(t *testing.T) {
	for _, tc := range []struct {
		Name      string
		Cascade   bool
		Status    int
		Body      string
		WantQuery string
		WantErr   string
	}{
		{"Deleted", false, http.StatusOK, `{"status":"ok"}`, "", ""},
		{"NotEmpty", false, http.StatusConflict, `{"error":"only empty projects can be deleted, and this one has services"}`, "",
			"Conflict: only empty projects can be deleted, and this one has services"},
		{"Cascade", true, http.StatusOK, `{"status":"ok"}`, "cascade=true", ""},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			var query string
			client, requests := newEnvTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				query = r.URL.RawQuery
				w.WriteHeader(tc.Status)
				w.Write([]byte(tc.Body))
			})
			err := client.DeleteProject("demo", tc.Cascade)
			if want := (recordedRequest{http.MethodDelete, "/projects/demo", ""}); (*requests)[0] != want {
				t.Errorf("request %+v, want %+v", (*requests)[0], want)
			}
			if query != tc.WantQuery {
				t.Errorf("query = %q, want %q", query, tc.WantQuery)
			}
			switch {
			case tc.WantErr == "" && err != nil:
				t.Errorf("unexpected error: %v", err)
			case tc.WantErr != "" && (err == nil || err.Error() != tc.WantErr):
				t.Errorf("error = %v, want %q", err, tc.WantErr)
			}
		})
	}
}

func TestDeleteService(t *testing.T) {
	client, requests := newEnvTestServer(t, func(w http.ResponseWriter, r *http.Request) {})
	if err := client.DeleteService("demo", "web"); err != nil {
		t.Fatalf("DeleteService: %v", err)
	}
	if want := (recordedRequest{http.MethodDelete, "/projects/demo/services/web", ""}); (*requests)[0] != want {
		t.Errorf("request %+v, want %+v", (*requests)[0], want)
	}
}
