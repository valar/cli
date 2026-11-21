package cmd

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/juju/ansiterm"
	"github.com/spf13/cobra"
	"github.com/valar/cli/api"
	"github.com/valar/cli/config"
)

var (
	trustProject string
	trustService string
	trustIssuer  string
	trustSubject string
	trustClaims  []string
	trustForce   bool
	trustGitHub  githubPolicy
)

// riskyEvents run with the repository's credentials on input that pull
// requests or comments control.
var riskyEvents = []string{"pull_request_target", "workflow_run", "issue_comment"}

// immutableSubject matches a GitHub subject that carries the repository's ID.
var immutableSubject = regexp.MustCompile(`^repo:[^:/]+@[0-9]+/[^:]+@[0-9]+(:|$)`)

var (
	trustCmd = &cobra.Command{
		Use:   "trust",
		Short: "Let CI jobs deploy without a stored token.",
		Long: `Let CI jobs deploy without a stored token.

A trust policy lets a CI job exchange its provider's identity token for a token
that acts as you, bound to one service with read and deploy, for an hour
('valar auth exchange'; the Valar GitHub Action does it on its own). Policies
need permission to manage the project and a token created in the console or
with authctl, and an installation that trusts the CI provider's identity
tokens. Deleting a policy revokes its tokens.`,
	}

	trustAddCmd = &cobra.Command{
		Use:     "add --service s (--github-repo owner/repo ... | --subject sub [--claim k=v]...)",
		Aliases: []string{"create"},
		Short:   "Add a trust policy for a service or for pull request previews.",
		Long: `Add a trust policy for a service or for pull request previews.

For GitHub Actions, --github-repo builds the policy: it reads the repository's
ID, its owner's ID and its subject format from GitHub, and pins both IDs, so a
renamed or re-registered repository or owner does not match. Give one of:

  --branch main --workflow deploy.yml
      Jobs of .github/workflows/deploy.yml on main, triggered by --event
      (push by default), may deploy the service.

  --environment production --workflow deploy.yml
      Jobs that name the environment in that workflow, run from the default
      branch, may deploy; --workflow deploy.yml@refs/heads/release names
      another ref. Protect the environment with deployment branch rules:
      GitHub issues its subject to every job that names it.

  --pull-requests
      Each pull request of the repository may deploy only the preview whose
      name is the --service prefix, which ends in *, followed by its number:
      --service 'webpr*' admits webpr42 to pull request 42. A private
      repository may give pull requests from forks identity tokens too.

  valar trust add --service web --github-repo acme/web --branch main --workflow deploy.yml
  valar trust add --service 'webpr*' --github-repo acme/web --pull-requests

For a private repository, or to read whether it uses immutable subjects, set
GH_TOKEN to a GitHub token that can read it, such as GH_TOKEN=$(gh auth token).

Without --github-repo, --subject is the identity token's sub claim, matched
exactly, and each --claim k=v another claim it must carry. --issuer defaults to
GitHub Actions' issuer and must be one the installation trusts.

Policies for pull_request_target, workflow_run and issue_comment jobs, which
run with the repository's credentials on input that others control, and
hand-written ones that admit pull requests to a service rather than to its
previews need --force.`,
		Args: cobra.NoArgs,
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			project, err := commandProject(trustProject)
			if err != nil {
				return err
			}
			req, err := newTrustPolicyRequest(trustService, trustIssuer, trustSubject, trustClaims, trustGitHub, trustForce, os.Stderr)
			if err != nil {
				return err
			}
			client, err := globalConfiguration.APIClient()
			if err != nil {
				return err
			}
			return addTrustPolicy(client, project, req, stdout, os.Stderr)
		}),
	}

	trustListCmd = &cobra.Command{
		Use:     "list [--project p]",
		Aliases: []string{"ls"},
		Short:   "List the project's trust policies, oldest first.",
		Args:    cobra.NoArgs,
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			project, err := commandProject(trustProject)
			if err != nil {
				return err
			}
			client, err := globalConfiguration.APIClient()
			if err != nil {
				return err
			}
			return listTrustPolicies(client, project, stdout, time.Now())
		}),
	}

	trustRemoveCmd = &cobra.Command{
		Use:     "remove [--project p] id",
		Aliases: []string{"delete", "rm"},
		Short:   "Remove a trust policy and revoke its tokens.",
		Args:    cobra.ExactArgs(1),
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			project, err := commandProject(trustProject)
			if err != nil {
				return err
			}
			client, err := globalConfiguration.APIClient()
			if err != nil {
				return err
			}
			return removeTrustPolicy(client, project, args[0], stdout)
		}),
	}
)

// commandProject is the --project flag, or the project of .valar.yml,
// VALAR_PROJECT or the active context.
func commandProject(flag string) (string, error) {
	project := flag
	if project == "" {
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, nil, globalConfiguration)
		if err != nil {
			return "", err
		}
		project = cfg.Project()
	}
	if err := api.VerifyProjectName(project); err != nil {
		return "", withKind(kindInvalid, err)
	}
	return project, nil
}

// newTrustPolicyRequest checks the flags of trust add and, with a GitHub
// repository, asks GitHub for the subject and claims. Without force, it
// refuses policies for risky events and hand-written ones that admit pull
// requests to a service. notes receives warnings and what it assumed.
func newTrustPolicyRequest(service, issuer, subject string, claimArgs []string, github githubPolicy, force bool, notes io.Writer) (api.TrustPolicyRequest, error) {
	req := api.TrustPolicyRequest{Service: service, Issuer: issuer}
	preview, isPreview := strings.CutSuffix(service, "*")
	switch {
	case service == "":
		return req, newError(kindInvalid, "--service is required: a service name, or a preview prefix followed by *")
	case api.VerifyServiceName(preview) != nil:
		return req, newError(kindInvalid, "--service %q is neither a service name nor a preview prefix followed by *", service)
	}
	claims, err := parseClaims(claimArgs)
	if err != nil {
		return req, err
	}
	if github.Repo == "" {
		if github.Branch != "" || github.Environment != "" || github.PullRequests || github.Workflow != "" {
			return req, newError(kindInvalid, "--branch, --environment, --pull-requests and --workflow need --github-repo")
		}
		if subject == "" {
			return req, newError(kindInvalid, "give --github-repo, or --subject with the identity token's sub claim")
		}
		if !isPreview && !force && slices.Contains(strings.Split(subject, ":"), "pull_request") {
			return req, newError(kindInvalid, "subject %s admits every pull request of the repository to %s; use a preview prefix such as --service '%spr*', or pass --force", subject, service, service)
		}
		if err := allowEvent(claims["event_name"], force); err != nil {
			return req, err
		}
		if strings.HasPrefix(issuer, api.GitHubIssuer) && claims["repository_id"] == "" && !immutableSubject.MatchString(subject) && !strings.Contains(subject, "repository_id:") {
			fmt.Fprintln(notes, "Warning: the policy pins no repository ID, so a repository that later takes over the name would match. Add --claim repository_id=<id>, or use --github-repo.")
		}
		req.Subject, req.Claims = subject, claims
		return req, nil
	}
	if err := github.check(isPreview, subject); err != nil {
		return req, err
	}
	if err := allowEvent(github.Event, force); err != nil {
		return req, err
	}
	sub, built, err := github.build(notes)
	if err != nil {
		return req, err
	}
	for k, v := range claims {
		if b, ok := built[k]; ok && b != v {
			return req, newError(kindInvalid, "--claim %s=%s contradicts %s=%s from GitHub", k, v, k, b)
		}
		built[k] = v
	}
	if err := allowEvent(built["event_name"], force); err != nil {
		return req, err
	}
	req.Subject, req.Claims = sub, built
	return req, nil
}

func allowEvent(event string, force bool) error {
	if force || !slices.Contains(riskyEvents, event) {
		return nil
	}
	return newError(kindInvalid, "%s jobs run with the repository's credentials on input that pull requests or comments control; pass --force to admit them anyway", event)
}

// check refuses flag combinations that build no sensible policy.
func (p githubPolicy) check(preview bool, subject string) error {
	kinds := 0
	for _, set := range []bool{p.Branch != "", p.Environment != "", p.PullRequests} {
		if set {
			kinds++
		}
	}
	switch {
	case subject != "":
		return newError(kindInvalid, "--github-repo writes the subject; leave out --subject")
	case kinds != 1:
		return newError(kindInvalid, "--github-repo needs exactly one of --branch, --environment and --pull-requests")
	case p.PullRequests && !preview:
		return newError(kindInvalid, "--pull-requests needs a preview prefix such as --service 'webpr*'")
	case p.PullRequests && (p.Workflow != "" || p.Event != "push"):
		return newError(kindInvalid, "--pull-requests pins no workflow or event; leave out --workflow and --event")
	case !p.PullRequests && preview:
		return newError(kindInvalid, "a preview prefix needs --pull-requests; branch and environment jobs have no pull request number")
	case !p.PullRequests && p.Workflow == "":
		return newError(kindInvalid, "--workflow is required with --branch and --environment, such as --workflow deploy.yml")
	case p.Event == "":
		return newError(kindInvalid, "--event must not be empty")
	}
	return nil
}

// parseClaims reads k=v pairs.
func parseClaims(args []string) (map[string]string, error) {
	if len(args) == 0 {
		return nil, nil
	}
	claims := make(map[string]string, len(args))
	for _, arg := range args {
		k, v, ok := strings.Cut(arg, "=")
		if !ok || k == "" {
			return nil, newError(kindInvalid, "--claim %q is not name=value", arg)
		}
		if _, dup := claims[k]; dup {
			return nil, newError(kindInvalid, "--claim %s is given twice", k)
		}
		claims[k] = v
	}
	return claims, nil
}

func addTrustPolicy(client *api.Client, project string, req api.TrustPolicyRequest, w, progress io.Writer) error {
	policy, err := client.CreateTrustPolicy(project, req)
	if err != nil {
		return err
	}
	return emit(w, policy, func(w io.Writer) error {
		if policy.Warning != "" {
			fmt.Fprintf(progress, "Warning: %s.\n", policy.Warning)
		}
		fmt.Fprintf(w, "Added trust policy %s for %s/%s.\n", policy.ID, project, policy.Service)
		tw := ansiterm.NewTabWriter(w, 6, 0, 1, ' ', 0)
		writeRows(tw, trustRows(*policy))
		return tw.Flush()
	})
}

func trustRows(p api.TrustPolicy) [][2]string {
	rows := [][2]string{{"Issuer", p.Issuer}, {"Subject", p.Subject}}
	for _, k := range sortedKeys(p.Claims) {
		rows = append(rows, [2]string{"Claim " + k, p.Claims[k]})
	}
	return rows
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func listTrustPolicies(client *api.Client, project string, w io.Writer, now time.Time) error {
	policies, err := client.ListTrustPolicies(project)
	if err != nil {
		return err
	}
	return emit(w, policies, func(w io.Writer) error {
		tw := ansiterm.NewTabWriter(w, 6, 0, 1, ' ', 0)
		fmt.Fprintln(tw, "ID\tSERVICE\tSUBJECT\tCLAIMS\tISSUER\tCREATED BY\tCREATED")
		for _, p := range policies {
			claims := make([]string, 0, len(p.Claims))
			for _, k := range sortedKeys(p.Claims) {
				claims = append(claims, k+"="+p.Claims[k])
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", p.ID, p.Service, p.Subject, orDash(strings.Join(claims, ",")),
				p.Issuer, p.CreatedBy, relative(p.CreatedAt, now))
		}
		return tw.Flush()
	})
}

func removeTrustPolicy(client *api.Client, project, id string, w io.Writer) error {
	if !idPattern.MatchString(id) {
		return newError(kindInvalid, "%q is not a trust policy ID; 'valar trust list' shows them", id)
	}
	n, err := client.DeleteTrustPolicy(project, id)
	if err != nil {
		return err
	}
	return emit(w, struct {
		ID      string `json:"id"`
		Revoked int    `json:"revoked"`
	}{id, n}, func(w io.Writer) error {
		tokens := "tokens"
		if n == 1 {
			tokens = "token"
		}
		_, err := fmt.Fprintf(w, "Removed trust policy %s and revoked %d %s.\n", id, n, tokens)
		return err
	})
}

func initTrustCmd() {
	add := trustAddCmd.Flags()
	add.StringVar(&trustService, "service", "", "Service the policy deploys, or a preview prefix followed by *")
	add.StringVar(&trustIssuer, "issuer", api.GitHubIssuer, "Issuer of the identity tokens")
	add.StringVar(&trustSubject, "subject", "", "The identity token's sub claim, matched exactly")
	add.StringArrayVar(&trustClaims, "claim", nil, "Another claim the identity token must carry, as name=value (repeatable)")
	add.StringVar(&trustGitHub.Repo, "github-repo", "", "Build the policy for this GitHub repository, as owner/repo")
	add.StringVar(&trustGitHub.Branch, "branch", "", "With --github-repo: admit jobs on this branch")
	add.StringVar(&trustGitHub.Environment, "environment", "", "With --github-repo: admit jobs that name this environment")
	add.BoolVar(&trustGitHub.PullRequests, "pull-requests", false, "With --github-repo: admit each pull request to its own preview")
	add.StringVar(&trustGitHub.Workflow, "workflow", "", "With --branch or --environment: the workflow file, such as deploy.yml")
	add.StringVar(&trustGitHub.Event, "event", "push", "With --branch or --environment: the event that triggers the workflow")
	add.BoolVar(&trustForce, "force", false, "Admit pull_request_target, workflow_run or issue_comment jobs, or pull requests to a service")
	for _, c := range []*cobra.Command{trustAddCmd, trustListCmd, trustRemoveCmd} {
		c.Flags().StringVar(&trustProject, "project", "", "Project of the policy, instead of the active one")
	}
	trustCmd.AddCommand(trustAddCmd, trustListCmd, trustRemoveCmd)
	rootCmd.AddCommand(trustCmd)
}
