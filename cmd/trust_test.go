package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/valar/cli/api"
)

const (
	policyID     = "00112233445566778899aabbccddeeff"
	policyJSON   = `{"id":"` + policyID + `","service":"web","issuer":"https://token.actions.githubusercontent.com","subject":"repo:acme/web:ref:refs/heads/main","claims":{"repository_id":"123","event_name":"push"},"createdBy":"alice","createdAt":"2026-11-21T10:00:00Z"}`
	previewJSON  = `{"id":"ffeeddccbbaa99887766554433221100","service":"webpr*","issuer":"https://token.actions.githubusercontent.com","subject":"repo:acme/web:pull_request","claims":{},"createdBy":"bob","createdAt":"2026-11-20T12:00:00Z"}`
	policiesPath = "/projects/acme/trust-policies"
)

// fakeGitHub serves GitHub API paths and records the Authorization header
// of each request.
func fakeGitHub(t *testing.T, routes map[string]string, statuses map[string]int) *[]string {
	t.Helper()
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))
		if status, ok := statuses[r.URL.Path]; ok {
			w.WriteHeader(status)
			w.Write([]byte(`{"message":"Not Found"}`))
			return
		}
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	prev := githubAPI
	githubAPI = srv.URL
	t.Cleanup(func() { githubAPI = prev })
	return &auth
}

const (
	githubRepoJSON = `{"id":123,"full_name":"Acme/Web","default_branch":"master","created_at":"2020-01-02T00:00:00Z","owner":{"id":45}}`
	newRepoJSON    = `{"id":123,"full_name":"Acme/Web","default_branch":"main","created_at":"2026-08-01T00:00:00Z","owner":{"id":45}}`
	repoPath       = "/repos/acme/web"
	subPath        = "/repos/Acme/Web/actions/oidc/customization/sub"
	defaultSub     = `{"use_default":true}`
)

func TestNewTrustPolicyRequest_GitHub(t *testing.T) {
	ids := map[string]string{"repository_id": "123", "repository_owner_id": "45"}
	with := func(extra map[string]string) map[string]string {
		m := map[string]string{}
		for k, v := range ids {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	for _, tc := range []struct {
		Name        string
		Service     string
		Policy      githubPolicy
		Claims      []string
		Repo, Sub   string
		SubStatus   int
		WantSubject string
		WantClaims  map[string]string
		WantNote    string
	}{
		{
			Name: "Branch", Service: "web",
			Policy: githubPolicy{Branch: "main", Workflow: "deploy.yml", Event: "push"},
			Repo:   githubRepoJSON, Sub: defaultSub,
			WantSubject: "repo:Acme/Web:ref:refs/heads/main",
			WantClaims:  with(map[string]string{"event_name": "push", "workflow_ref": "Acme/Web/.github/workflows/deploy.yml@refs/heads/main"}),
		},
		{
			Name: "EnvironmentImmutable", Service: "web",
			Policy: githubPolicy{Environment: "prod:eu", Workflow: ".github/workflows/cd.yml", Event: "workflow_dispatch"},
			Repo:   githubRepoJSON, Sub: `{"use_default":true,"use_immutable_subject":true}`,
			WantSubject: "repo:Acme@45/Web@123:environment:prod%3Aeu",
			WantClaims:  with(map[string]string{"event_name": "workflow_dispatch", "workflow_ref": "Acme/Web/.github/workflows/cd.yml@refs/heads/master"}),
		},
		{
			Name: "EnvironmentFromRef", Service: "web",
			Policy: githubPolicy{Environment: "prod", Workflow: "cd.yml@release", Event: "push"},
			Repo:   githubRepoJSON, Sub: `{"use_default":false,"include_claim_keys":["repo","context"]}`,
			WantSubject: "repo:Acme/Web:environment:prod",
			WantClaims:  with(map[string]string{"event_name": "push", "workflow_ref": "Acme/Web/.github/workflows/cd.yml@refs/heads/release"}),
		},
		{
			Name: "PullRequests", Service: "webpr*",
			Policy: githubPolicy{PullRequests: true, Event: "push"},
			Claims: []string{"repository_visibility=private"},
			Repo:   githubRepoJSON, Sub: defaultSub,
			WantSubject: "repo:Acme/Web:pull_request",
			WantClaims:  with(map[string]string{"repository_visibility": "private"}),
		},
		{
			Name: "Prefix", Service: "webpr*",
			Policy: githubPolicy{PullRequests: true, Event: "push"},
			Repo:   newRepoJSON, Sub: `{"use_default":true,"use_immutable_subject":true,"sub_claim_prefix":"repo:Acme@45/Web@123"}`,
			WantSubject: "repo:Acme@45/Web@123:pull_request",
			WantClaims:  ids,
		},
		{
			Name: "CustomKeys", Service: "web",
			Policy: githubPolicy{Branch: "main", Workflow: "deploy.yml", Event: "push"},
			Repo:   githubRepoJSON, Sub: `{"use_default":false,"include_claim_keys":["repository_owner_id","repository_id","context"],"sub_claim_prefix":"repo:Acme/Web"}`,
			WantSubject: "repository_owner_id:45:repository_id:123:ref:refs/heads/main",
			WantClaims:  with(map[string]string{"event_name": "push", "workflow_ref": "Acme/Web/.github/workflows/deploy.yml@refs/heads/main"}),
		},
		{
			Name: "CustomKeysForEnvironment", Service: "web",
			Policy: githubPolicy{Environment: "prod", Workflow: "deploy.yml", Event: "push"},
			Repo:   githubRepoJSON, Sub: `{"use_default":false,"include_claim_keys":["repo","environment","workflow_ref"],"sub_claim_prefix":"repo:Acme/Web"}`,
			WantSubject: "repo:Acme/Web:environment:prod:workflow_ref:Acme/Web/.github/workflows/deploy.yml@refs/heads/master",
			WantClaims:  with(map[string]string{"event_name": "push", "workflow_ref": "Acme/Web/.github/workflows/deploy.yml@refs/heads/master"}),
		},
		{
			Name: "NewRepoUnreadableSettings", Service: "webpr*",
			Policy: githubPolicy{PullRequests: true, Event: "push"},
			Repo:   newRepoJSON, SubStatus: http.StatusNotFound,
			WantSubject: "repo:Acme@45/Web@123:pull_request",
			WantClaims:  ids,
			WantNote:    "assumes the default, immutable format of a repository created 2026-08-01",
		},
		{
			Name: "OldRepoUnreadableSettings", Service: "web",
			Policy: githubPolicy{Branch: "main", Workflow: "deploy.yml", Event: "push"},
			Repo:   githubRepoJSON, SubStatus: http.StatusForbidden,
			WantSubject: "repo:Acme/Web:ref:refs/heads/main",
			WantClaims:  with(map[string]string{"event_name": "push", "workflow_ref": "Acme/Web/.github/workflows/deploy.yml@refs/heads/main"}),
			WantNote:    "assumes the default, name-based format",
		},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			routes := map[string]string{repoPath: tc.Repo, subPath: tc.Sub}
			statuses := map[string]int{}
			if tc.SubStatus != 0 {
				statuses[subPath] = tc.SubStatus
			}
			t.Setenv("GH_TOKEN", "gh-secret")
			auth := fakeGitHub(t, routes, statuses)
			tc.Policy.Repo = "acme/web"
			var notes bytes.Buffer
			req, err := newTrustPolicyRequest(tc.Service, api.GitHubIssuer, "", tc.Claims, tc.Policy, &notes)
			if err != nil {
				t.Fatalf("newTrustPolicyRequest: %v", err)
			}
			want := api.TrustPolicyRequest{Service: tc.Service, Issuer: api.GitHubIssuer, Subject: tc.WantSubject, Claims: tc.WantClaims}
			if !reflect.DeepEqual(req, want) {
				t.Errorf("request\n got %+v\nwant %+v", req, want)
			}
			if tc.WantNote == "" && notes.Len() != 0 || !strings.Contains(notes.String(), tc.WantNote) {
				t.Errorf("notes %q, want %q", notes.String(), tc.WantNote)
			}
			for _, a := range *auth {
				if a != "Bearer gh-secret" {
					t.Errorf("GitHub got Authorization %q, want GH_TOKEN", a)
				}
			}
		})
	}
}

func TestNewTrustPolicyRequest_Refused(t *testing.T) {
	branch := githubPolicy{Repo: "acme/web", Branch: "main", Workflow: "deploy.yml", Event: "push"}
	for _, tc := range []struct {
		Name      string
		Service   string
		Subject   string
		Claims    []string
		Policy    githubPolicy
		Repo, Sub string
		WantKind  errorKind
		Want      string
	}{
		{Name: "NoService", Subject: "s", WantKind: kindInvalid, Want: "--service is required"},
		{Name: "BadService", Service: "web-1", Subject: "s", WantKind: kindInvalid, Want: "neither a service name"},
		{Name: "StarInside", Service: "w*b", Subject: "s", WantKind: kindInvalid, Want: "neither a service name"},
		{Name: "NoSubject", Service: "web", WantKind: kindInvalid, Want: "give --github-repo, or --subject"},
		{Name: "BadClaim", Service: "web", Subject: "s", Claims: []string{"novalue"}, WantKind: kindInvalid, Want: "is not name=value"},
		{Name: "TwiceClaim", Service: "web", Subject: "s", Claims: []string{"a=1", "a=2"}, WantKind: kindInvalid, Want: "given twice"},
		{Name: "BranchWithoutRepo", Service: "web", Subject: "s", Policy: githubPolicy{Branch: "main"}, WantKind: kindInvalid, Want: "need --github-repo"},
		{Name: "RepoAndSubject", Service: "web", Subject: "s", Policy: branch, WantKind: kindInvalid, Want: "leave out --subject"},
		{Name: "NoKind", Service: "web", Policy: githubPolicy{Repo: "acme/web", Workflow: "d.yml", Event: "push"}, WantKind: kindInvalid, Want: "exactly one of"},
		{Name: "TwoKinds", Service: "web", Policy: githubPolicy{Repo: "acme/web", Branch: "main", Environment: "prod", Workflow: "d.yml", Event: "push"}, WantKind: kindInvalid, Want: "exactly one of"},
		{Name: "PullRequestsForService", Service: "web", Policy: githubPolicy{Repo: "acme/web", PullRequests: true, Event: "push"}, WantKind: kindInvalid, Want: "needs a preview prefix"},
		{Name: "PullRequestsWorkflow", Service: "webpr*", Policy: githubPolicy{Repo: "acme/web", PullRequests: true, Workflow: "d.yml", Event: "push"}, WantKind: kindInvalid, Want: "pins no workflow"},
		{Name: "BranchForPreviews", Service: "webpr*", Policy: branch, WantKind: kindInvalid, Want: "needs --pull-requests"},
		{Name: "NoWorkflow", Service: "web", Policy: githubPolicy{Repo: "acme/web", Branch: "main", Event: "push"}, WantKind: kindInvalid, Want: "--workflow is required"},
		{Name: "BadRepo", Service: "web", Policy: githubPolicy{Repo: "acme", Branch: "main", Workflow: "d.yml", Event: "push"}, WantKind: kindInvalid, Want: "is not owner/repo"},
		{Name: "UnknownRepo", Service: "web", Policy: branch, WantKind: kindNotFound, Want: "GitHub has no repository acme/web"},
		{
			Name: "CustomSubject", Service: "web", Policy: branch, Repo: githubRepoJSON,
			Sub:      `{"use_default":false,"include_claim_keys":["repo","context","job_workflow_ref"]}`,
			WantKind: kindInvalid, Want: "puts job_workflow_ref in its OIDC subject (repo, context, job_workflow_ref)",
		},
		{
			Name: "PreviewWithWorkflowInSubject", Service: "webpr*", Policy: githubPolicy{Repo: "acme/web", PullRequests: true, Event: "push"}, Repo: githubRepoJSON,
			Sub:      `{"use_default":false,"include_claim_keys":["repo","context","workflow_ref"]}`,
			WantKind: kindInvalid, Want: "puts workflow_ref in its OIDC subject",
		},
		{
			Name: "ContradictingClaim", Service: "web", Policy: branch, Claims: []string{"event_name=workflow_dispatch"}, Repo: githubRepoJSON, Sub: defaultSub,
			WantKind: kindInvalid, Want: "contradicts event_name=push",
		},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			routes := map[string]string{subPath: tc.Sub}
			if tc.Repo != "" {
				routes[repoPath] = tc.Repo
			}
			fakeGitHub(t, routes, nil)
			if tc.Policy.Event == "" && tc.Policy.Repo != "" {
				tc.Policy.Event = "push"
			}
			_, err := newTrustPolicyRequest(tc.Service, api.GitHubIssuer, tc.Subject, tc.Claims, tc.Policy, io.Discard)
			if err == nil || !strings.Contains(err.Error(), tc.Want) || classify(err, kindFailed) != tc.WantKind {
				t.Errorf("error %v of kind %v, want kind %v containing %q", err, classify(err, kindFailed), tc.WantKind, tc.Want)
			}
		})
	}
}

func TestNewTrustPolicyRequest_ByHand(t *testing.T) {
	req, err := newTrustPolicyRequest("web", "https://gitlab.example.com", "project_path:acme/web:ref_type:branch:ref:main",
		[]string{"project_id=7", "namespace_id=3"}, githubPolicy{Event: "push"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	want := api.TrustPolicyRequest{
		Service: "web", Issuer: "https://gitlab.example.com", Subject: "project_path:acme/web:ref_type:branch:ref:main",
		Claims: map[string]string{"project_id": "7", "namespace_id": "3"},
	}
	if !reflect.DeepEqual(req, want) {
		t.Errorf("request %+v, want %+v", req, want)
	}
}

func TestAddTrustPolicy(t *testing.T) {
	warned := strings.Replace(policyJSON, `"createdBy"`, `"warning":"rules allow service::acme, which the service account of every preview inherits","createdBy"`, 1)
	setJSON(t, false)
	client, requests := newFakeAPI(t, map[string]string{"POST " + policiesPath: warned})
	var out, progress bytes.Buffer
	req := api.TrustPolicyRequest{Service: "web", Subject: "repo:acme/web:ref:refs/heads/main", Issuer: api.GitHubIssuer, Claims: map[string]string{"repository_id": "123"}}
	if err := addTrustPolicy(client, "acme", req, &out, &progress); err != nil {
		t.Fatalf("addTrustPolicy: %v", err)
	}
	if got, want := (*requests)[0].Body, `{"service":"web","subject":"repo:acme/web:ref:refs/heads/main","issuer":"https://token.actions.githubusercontent.com","claims":{"repository_id":"123"}}`; got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
	want := []string{
		"Added trust policy " + policyID + " for acme/web.",
		"Issuer: https://token.actions.githubusercontent.com",
		"Subject: repo:acme/web:ref:refs/heads/main",
		"Claim event_name: push",
		"Claim repository_id: 123",
	}
	if got := lines(out.String()); !reflect.DeepEqual(got, want) {
		t.Errorf("printed\n got %q\nwant %q", got, want)
	}
	if got := progress.String(); got != "Warning: rules allow service::acme, which the service account of every preview inherits.\n" {
		t.Errorf("progress %q, want the warning", got)
	}
}

func TestTrustPolicies_Refused(t *testing.T) {
	for _, tc := range []struct {
		Name     string
		Status   int
		Body     string
		WantKind errorKind
		Want     string
	}{
		{"Duplicate", http.StatusConflict, `{"error":"an identical trust policy exists"}`, kindConflict, "an identical trust policy exists"},
		{"Child", http.StatusForbidden, `{"error":"only tokens minted in the console or with authctl can create trust policies"}`, kindNotAllowed, "only tokens minted in the console"},
		{"Disabled", http.StatusNotFound, `{"error":"deploys from CI are not enabled on this installation"}`, kindNotFound, "deploys from CI are not enabled"},
		{"BadIssuer", http.StatusBadRequest, `{"error":"issuer must be one of https://token.actions.githubusercontent.com"}`, kindInvalid, "issuer must be one of"},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			setJSON(t, false)
			client, _ := newFakeAPIWithStatus(t, map[string]string{"POST " + policiesPath: tc.Body}, map[string]int{"POST " + policiesPath: tc.Status})
			var out, progress bytes.Buffer
			err := addTrustPolicy(client, "acme", api.TrustPolicyRequest{Service: "web", Subject: "s"}, &out, &progress)
			if err == nil || !strings.Contains(err.Error(), tc.Want) || classify(err, kindFailed) != tc.WantKind {
				t.Errorf("error %v of kind %v, want kind %v containing %q", err, classify(err, kindFailed), tc.WantKind, tc.Want)
			}
			if out.Len() != 0 || progress.Len() != 0 {
				t.Errorf("printed %q and %q on a refusal", out.String(), progress.String())
			}
		})
	}
}

func TestListTrustPolicies(t *testing.T) {
	setJSON(t, false)
	client, _ := newFakeAPI(t, map[string]string{"GET " + policiesPath: "[" + policyJSON + "," + previewJSON + "]"})
	var out bytes.Buffer
	if err := listTrustPolicies(client, "acme", &out, tokenNow); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"ID SERVICE SUBJECT CLAIMS ISSUER CREATED BY CREATED",
		policyID + " web repo:acme/web:ref:refs/heads/main event_name=push,repository_id=123 https://token.actions.githubusercontent.com alice 2 hours ago",
		"ffeeddccbbaa99887766554433221100 webpr* repo:acme/web:pull_request - https://token.actions.githubusercontent.com bob 1 day ago",
	}
	if got := lines(out.String()); !reflect.DeepEqual(got, want) {
		t.Errorf("table\n got %q\nwant %q", got, want)
	}
}

func TestRemoveTrustPolicy(t *testing.T) {
	for _, tc := range []struct {
		Name     string
		ID       string
		Revoked  string
		Want     string
		WantKind errorKind
	}{
		{Name: "None", ID: policyID, Revoked: `{"revoked":0}`, Want: "Removed trust policy " + policyID + " and revoked 0 tokens."},
		{Name: "One", ID: policyID, Revoked: `{"revoked":1}`, Want: "Removed trust policy " + policyID + " and revoked 1 token."},
		{Name: "BadID", ID: "web", WantKind: kindInvalid, Want: "is not a trust policy ID"},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			setJSON(t, false)
			client, requests := newFakeAPI(t, map[string]string{"DELETE " + policiesPath + "/" + policyID: tc.Revoked})
			var out bytes.Buffer
			err := removeTrustPolicy(client, "acme", tc.ID, &out)
			if tc.WantKind != 0 {
				if err == nil || !strings.Contains(err.Error(), tc.Want) || classify(err, kindFailed) != tc.WantKind {
					t.Errorf("error %v, want kind %v containing %q", err, tc.WantKind, tc.Want)
				}
				if len(*requests) != 0 {
					t.Errorf("sent %v for an invalid ID", *requests)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(out.String()); got != tc.Want {
				t.Errorf("printed %q, want %q", got, tc.Want)
			}
		})
	}
}

func TestTrustCommands_OlderServer(t *testing.T) {
	setJSON(t, false)
	client, _ := newFakeAPI(t, nil)
	for name, err := range map[string]error{
		"add":    addTrustPolicy(client, "acme", api.TrustPolicyRequest{Service: "web", Subject: "s"}, io.Discard, io.Discard),
		"list":   listTrustPolicies(client, "acme", io.Discard, tokenNow),
		"remove": removeTrustPolicy(client, "acme", policyID, io.Discard),
	} {
		if !errors.Is(err, api.ErrNoTrustAPI) {
			t.Errorf("%s: error = %v, want ErrNoTrustAPI", name, err)
		}
	}
}

// runTrustCLI runs the root command and resets the trust flags afterwards.
func runTrustCLI(t *testing.T, endpoint string, args ...string) string {
	t.Helper()
	t.Cleanup(func() {
		trustProject, trustService, trustIssuer, trustSubject, trustClaims = "", "", api.GitHubIssuer, "", nil
		trustGitHub = githubPolicy{Event: "push"}
	})
	return runCLI(t, endpoint, args...)
}

func TestTrustCommands_Flags(t *testing.T) {
	for _, tc := range []struct {
		Name     string
		Args     []string
		Route    string
		Response string
		WantBody string
	}{
		{
			"Add",
			[]string{"trust", "add", "--service", "web", "--subject", "repo:acme/web:ref:refs/heads/main", "--claim", "repository_id=123"},
			"POST /projects/p/trust-policies", policyJSON,
			`{"service":"web","subject":"repo:acme/web:ref:refs/heads/main","issuer":"https://token.actions.githubusercontent.com","claims":{"repository_id":"123"}}`,
		},
		{
			"CreateWithIssuer",
			[]string{"trust", "create", "--project", "acme", "--service", "webpr*", "--subject", "x", "--issuer", "https://gitlab.example.com"},
			"POST " + policiesPath, previewJSON, `{"service":"webpr*","subject":"x","issuer":"https://gitlab.example.com"}`,
		},
		{"List", []string{"trust", "list", "--project", "acme"}, "GET " + policiesPath, "[" + policyJSON + "]", ""},
		{"Delete", []string{"trust", "delete", policyID}, "DELETE /projects/p/trust-policies/" + policyID, `{"revoked":2}`, ""},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			writeServiceConfig(t, deployConfig)
			client, requests := newFakeAPI(t, map[string]string{tc.Route: tc.Response})
			out := runTrustCLI(t, client.Endpoint, tc.Args...)
			if got := routeOrder(*requests); len(got) != 1 || got[0] != tc.Route {
				t.Fatalf("requests %v, want [%s]", got, tc.Route)
			}
			if tc.WantBody != "" && (*requests)[0].Body != tc.WantBody {
				t.Errorf("body = %s, want %s", (*requests)[0].Body, tc.WantBody)
			}
			if !json.Valid([]byte(out)) {
				t.Errorf("output %q is not JSON", out)
			}
		})
	}
}

func TestWorkflowRef(t *testing.T) {
	for _, tc := range []struct{ Workflow, Branch, Want string }{
		{"deploy.yml", "main", "acme/web/.github/workflows/deploy.yml@refs/heads/main"},
		{".github/workflows/deploy.yml", "main", "acme/web/.github/workflows/deploy.yml@refs/heads/main"},
		{"deploy.yml@release", "main", "acme/web/.github/workflows/deploy.yml@refs/heads/release"},
		{"deploy.yml@refs/tags/v1", "main", "acme/web/.github/workflows/deploy.yml@refs/tags/v1"},
	} {
		if got := workflowRef("acme/web", tc.Workflow, tc.Branch); got != tc.Want {
			t.Errorf("workflowRef(%q, %q) = %q, want %q", tc.Workflow, tc.Branch, got, tc.Want)
		}
	}
}
