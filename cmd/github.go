package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// githubAPI is GitHub's REST API; tests replace it.
var githubAPI = "https://api.github.com"

// immutableSubjectsSince is when new, renamed and transferred repositories
// started to get immutable subjects.
var immutableSubjectsSince = time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)

// githubRepo is what a trust policy needs to know about a repository.
type githubRepo struct {
	ID            int64     `json:"id"`
	FullName      string    `json:"full_name"`
	DefaultBranch string    `json:"default_branch"`
	CreatedAt     time.Time `json:"created_at"`
	Owner         struct {
		ID int64 `json:"id"`
	} `json:"owner"`
}

// githubSubjectTemplate is a repository's OIDC subject customization.
type githubSubjectTemplate struct {
	UseDefault          bool     `json:"use_default"`
	IncludeClaimKeys    []string `json:"include_claim_keys"`
	UseImmutableSubject bool     `json:"use_immutable_subject"`
	// SubClaimPrefix is the subject's repo part, such as repo:acme@45/web@123.
	SubClaimPrefix string `json:"sub_claim_prefix"`
}

// githubError is a GitHub answer other than 200.
type githubError struct {
	Status int
	Text   string
}

func (e githubError) Error() string { return "GitHub answered " + e.Text }

// githubStatus returns the status of a GitHub answer other than 200, or 0.
func githubStatus(err error) int {
	var gerr githubError
	if errors.As(err, &gerr) {
		return gerr.Status
	}
	return 0
}

// githubGet reads a GitHub API path with GH_TOKEN or GITHUB_TOKEN, if set.
func githubGet(path string, v any) error {
	req, err := http.NewRequest(http.MethodGet, githubAPI+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "valar-cli")
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if token := os.Getenv(name); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
			break
		}
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("asking GitHub: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("asking GitHub: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return githubError{resp.StatusCode, resp.Status + " for " + path}
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("reading GitHub's answer for %s: %w", path, err)
	}
	return nil
}

// githubPolicy describes a GitHub Actions trust policy: a repository with
// one of a branch, an environment or its pull requests.
type githubPolicy struct {
	Repo         string
	Branch       string
	Environment  string
	PullRequests bool
	// Workflow is a file in .github/workflows or a path, optionally followed
	// by @ and the ref it runs from.
	Workflow string
	Event    string
}

// build reads the repository's IDs and subject format from GitHub and
// returns the policy's subject and claims. notes receives what it assumed.
func (p githubPolicy) build(notes io.Writer) (string, map[string]string, error) {
	owner, name, ok := strings.Cut(p.Repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", nil, newError(kindInvalid, "--github-repo %q is not owner/repo", p.Repo)
	}
	var repo githubRepo
	if err := githubGet("/repos/"+owner+"/"+name, &repo); githubStatus(err) == http.StatusNotFound {
		return "", nil, newError(kindNotFound, "GitHub has no repository %s that this CLI can read; for a private one, set GH_TOKEN to a token that can read it", p.Repo)
	} else if err != nil {
		return "", nil, withKind(kindUnavailable, err)
	}
	owner, name, _ = strings.Cut(repo.FullName, "/")
	tmpl := githubSubjectTemplate{UseDefault: true, UseImmutableSubject: !repo.CreatedAt.Before(immutableSubjectsSince)}
	err := githubGet("/repos/"+repo.FullName+"/actions/oidc/customization/sub", &tmpl)
	switch status := githubStatus(err); {
	case status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusNotFound:
		format := "name-based"
		if tmpl.UseImmutableSubject {
			format = "immutable"
		}
		fmt.Fprintf(notes, "Could not read the OIDC subject settings of %s, so the subject assumes the default, %s format of a repository created %s. "+
			"Set GH_TOKEN to a token that can read the repository's Actions settings to check, or pass --subject.\n",
			repo.FullName, format, repo.CreatedAt.Format("2006-01-02"))
	case err != nil:
		return "", nil, withKind(kindUnavailable, err)
	}
	prefix := tmpl.SubClaimPrefix
	switch {
	case prefix != "":
	case tmpl.UseImmutableSubject:
		prefix = fmt.Sprintf("repo:%s@%d/%s@%d", owner, repo.Owner.ID, name, repo.ID)
	default:
		prefix = "repo:" + repo.FullName
	}
	repoID, ownerID := strconv.FormatInt(repo.ID, 10), strconv.FormatInt(repo.Owner.ID, 10)
	claims := map[string]string{"repository_id": repoID, "repository_owner_id": ownerID}
	// Each subject key GitHub can write, with its value for this policy.
	values := map[string]string{
		"repository": repo.FullName, "repository_owner": owner,
		"repository_id": repoID, "repository_owner_id": ownerID,
	}
	// scope is the claim that tells this policy's jobs apart: the branch's
	// ref or the environment.
	context, scope := "pull_request", ""
	if !p.PullRequests {
		ref := repo.DefaultBranch
		if p.Branch != "" {
			ref, scope = p.Branch, "ref"
			values["ref"] = "refs/heads/" + p.Branch
			context = "ref:refs/heads/" + subjectValue(p.Branch)
		} else {
			scope = "environment"
			values["environment"] = p.Environment
			context = "environment:" + subjectValue(p.Environment)
		}
		claims["event_name"] = p.Event
		claims["workflow_ref"] = workflowRef(repo.FullName, p.Workflow, ref)
		values["event_name"], values["workflow_ref"] = claims["event_name"], claims["workflow_ref"]
	}
	keys := []string{"repo", "context"}
	if !tmpl.UseDefault && len(tmpl.IncludeClaimKeys) > 0 {
		keys = tmpl.IncludeClaimKeys
	}
	// A subject without the context would admit jobs of any branch or
	// environment, so the claim pins it instead.
	if scope != "" && !slices.Contains(keys, "context") && !slices.Contains(keys, scope) {
		claims[scope] = values[scope]
	}
	parts := make([]string, len(keys))
	for i, key := range keys {
		value, known := values[key]
		switch {
		case key == "repo":
			parts[i] = prefix
		case key == "context":
			parts[i] = context
		case known:
			parts[i] = key + ":" + subjectValue(value)
		default:
			return "", nil, newError(kindInvalid, "%s puts %s in its OIDC subject (%s), which this policy cannot know; pass --subject and --claim instead of --github-repo",
				repo.FullName, key, strings.Join(keys, ", "))
		}
	}
	return strings.Join(parts, ":"), claims, nil
}

// subjectValue escapes a value as GitHub does in subjects.
func subjectValue(s string) string {
	return strings.ReplaceAll(s, ":", "%3A")
}

// workflowRef writes the workflow_ref claim of a workflow file that runs from
// branch, unless workflow names its own ref after @.
func workflowRef(repo, workflow, branch string) string {
	path, ref, ok := strings.Cut(workflow, "@")
	switch {
	case !ok:
		ref = "refs/heads/" + branch
	case !strings.HasPrefix(ref, "refs/"):
		ref = "refs/heads/" + ref
	}
	if !strings.Contains(path, "/") {
		path = ".github/workflows/" + path
	}
	return repo + "/" + path + "@" + ref
}
