package cmd

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/valar/cli/api"
	"github.com/valar/cli/config"
	"golang.org/x/term"
)

// maxIDToken is the largest identity token the platform accepts.
const maxIDToken = 16 << 10

// GitHub Actions sets these in jobs with permissions: id-token: write.
const (
	envIDTokenRequestURL   = "ACTIONS_ID_TOKEN_REQUEST_URL"
	envIDTokenRequestToken = "ACTIONS_ID_TOKEN_REQUEST_TOKEN"
)

var (
	exchangeService     string
	exchangeProject     string
	exchangeIDToken     string
	exchangeIDTokenFile string
	exchangeAudience    string
	exchangeExport      bool
)

var authExchangeCmd = &cobra.Command{
	Use:   "exchange [--service s] [--project p] [--id-token-file path | --id-token jwt] [--audience aud] [--export]",
	Short: "Exchange a CI job's identity token for a token for one service.",
	Long: `Exchange a CI job's identity token for a token for one service.

A trust policy of the project ('valar trust add') must admit the identity
token. The token Valar returns acts as the policy's creator, bound to the
service with read and deploy, and lasts an hour. The service and project come
from the flags, VALAR_PROJECT or .valar.yml, as for other commands.

The identity token comes from --id-token, from --id-token-file (- for stdin),
or, in a GitHub Actions job with permissions: id-token: write, from GitHub for
--audience, which defaults to the endpoint's origin (https://api.valar.dev).
Otherwise it is read from stdin.

The token goes to stdout and everything else to stderr:

  VALAR_TOKEN=$(valar auth exchange --service web)
  export VALAR_TOKEN

In GitHub Actions the token is masked in the log, and --export sets
VALAR_TOKEN for the job's later steps instead of printing it.`,
	Args: cobra.NoArgs,
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		project, service := exchangeProject, exchangeService
		if project == "" || service == "" {
			cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &service, globalConfiguration)
			if err != nil {
				return err
			}
			if project == "" {
				project = cfg.Project()
			}
			service = cfg.Service()
		}
		if err := api.VerifyNames(project, service); err != nil {
			return withKind(kindInvalid, err)
		}
		endpoint := globalConfiguration.EndpointOrDefault()
		idToken, err := readIDToken(exchangeIDToken, exchangeIDTokenFile, exchangeAudience, endpoint, os.Stdin)
		if err != nil {
			return err
		}
		client, err := api.NewClient(endpoint, "")
		if err != nil {
			return err
		}
		return exchangeToken(client, project, service, idToken, exchangeExport, stdout, os.Stderr)
	}),
}

// readIDToken returns the identity token from the flag, the file, GitHub
// Actions or stdin, in that order.
func readIDToken(value, file, audience, endpoint string, stdin *os.File) (string, error) {
	switch {
	case value != "" && file != "":
		return "", newError(kindInvalid, "give --id-token or --id-token-file, not both")
	case value != "":
		return strings.TrimSpace(value), nil
	case file == "-":
		return readIDTokenFrom(stdin, "stdin")
	case file != "":
		f, err := os.Open(file)
		if err != nil {
			return "", withKind(kindInvalid, err)
		}
		defer f.Close()
		return readIDTokenFrom(f, file)
	}
	if requestURL := os.Getenv(envIDTokenRequestURL); requestURL != "" {
		if audience == "" {
			u, err := url.Parse(endpoint)
			if err != nil || u.Host == "" {
				return "", newError(kindInvalid, "endpoint %q has no origin to use as the audience; pass --audience", endpoint)
			}
			audience = u.Scheme + "://" + u.Host
		}
		return githubIDToken(requestURL, os.Getenv(envIDTokenRequestToken), audience)
	}
	if term.IsTerminal(int(stdin.Fd())) {
		return "", newError(kindInvalid, "no identity token: pass --id-token-file, pipe it to stdin, or run in a GitHub Actions job with permissions: id-token: write")
	}
	return readIDTokenFrom(stdin, "stdin")
}

func readIDTokenFrom(r io.Reader, name string) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxIDToken+1))
	if err != nil {
		return "", fmt.Errorf("reading the identity token from %s: %w", name, err)
	}
	token := strings.TrimSpace(string(data))
	switch {
	case token == "":
		return "", newError(kindInvalid, "no identity token in %s", name)
	case len(data) > maxIDToken:
		return "", newError(kindInvalid, "the identity token in %s is larger than 16 KiB", name)
	}
	return token, nil
}

// githubIDToken requests a GitHub Actions identity token for audience.
func githubIDToken(requestURL, requestToken, audience string) (string, error) {
	sep := "?"
	if strings.Contains(requestURL, "?") {
		sep = "&"
	}
	req, err := http.NewRequest(http.MethodGet, requestURL+sep+"audience="+url.QueryEscape(audience), nil)
	if err != nil {
		return "", fmt.Errorf("requesting an identity token from GitHub: %w", err)
	}
	req.Header.Set("Authorization", "bearer "+requestToken)
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: time.Minute}).Do(req)
	if err != nil {
		return "", withKind(kindUnavailable, fmt.Errorf("requesting an identity token from GitHub: %w", err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", withKind(kindUnavailable, fmt.Errorf("requesting an identity token from GitHub: %w", err))
	}
	if resp.StatusCode != http.StatusOK {
		return "", withKind(kindUnavailable, fmt.Errorf("requesting an identity token from GitHub: %s", resp.Status))
	}
	var doc struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || doc.Value == "" {
		return "", withKind(kindUnavailable, errors.New("GitHub returned no identity token"))
	}
	return doc.Value, nil
}

// exchangeToken trades idToken for a token and writes it to w, or with export
// to the file GITHUB_ENV names.
func exchangeToken(client *api.Client, project, service, idToken string, export bool, w, progress io.Writer) error {
	githubEnv := os.Getenv("GITHUB_ENV")
	if export && githubEnv == "" {
		return newError(kindInvalid, "--export works in GitHub Actions, which sets GITHUB_ENV; elsewhere capture the printed token, as in VALAR_TOKEN=$(valar auth exchange)")
	}
	token, err := client.ExchangeToken(project, service, idToken)
	if apiErr := (api.Error{}); errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden {
		if hint := identityHint(idToken); hint != "" {
			err = fmt.Errorf("%w\n%s", err, hint)
		}
	}
	if err != nil {
		return err
	}
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		fmt.Fprintf(progress, "::add-mask::%s\n", token.Token)
	}
	expiry := token.ExpiresAt.UTC().Format("2006-01-02 15:04 MST")
	if export {
		if err := appendGitHubEnv(githubEnv, "VALAR_TOKEN", token.Token); err != nil {
			return err
		}
		return emit(w, struct {
			ExpiresAt time.Time `json:"expiresAt"`
		}{token.ExpiresAt}, func(io.Writer) error {
			_, err := fmt.Fprintf(progress, "Set VALAR_TOKEN for the job's later steps: a token for %s/%s until %s.\n", project, service, expiry)
			return err
		})
	}
	return emit(w, token, func(w io.Writer) error {
		fmt.Fprintf(progress, "Exchanged the identity token for a token for %s/%s until %s.\n", project, service, expiry)
		_, err := fmt.Fprintln(w, token.Token)
		return err
	})
}

func appendGitHubEnv(path, name, value string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("export %s: %w", name, err)
	}
	if _, err := fmt.Fprintf(f, "%s=%s\n", name, value); err != nil {
		f.Close()
		return fmt.Errorf("export %s: %w", name, err)
	}
	return f.Close()
}

// identityHint shows the claims a trust policy matches, read without
// verification from the identity token, so a refusal can be compared with
// 'valar trust list'.
func identityHint(idToken string) string {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]any
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	var lines []string
	for _, k := range []string{"iss", "sub", "ref", "repository_id", "repository_owner_id", "event_name", "workflow_ref"} {
		if v, ok := claims[k].(string); ok {
			lines = append(lines, fmt.Sprintf("  %s: %s", k, v))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "The identity token carries:\n" + strings.Join(lines, "\n")
}

func initExchangeCmd() {
	f := authExchangeCmd.Flags()
	f.StringVarP(&exchangeService, "service", "s", "", "Service to get a token for, instead of the one in .valar.yml")
	f.StringVarP(&exchangeProject, "project", "p", "", "Project of the service, instead of the active one")
	f.StringVar(&exchangeIDToken, "id-token", "", "The identity token")
	f.StringVar(&exchangeIDTokenFile, "id-token-file", "", "Read the identity token from this file, or - for stdin")
	f.StringVar(&exchangeAudience, "audience", "", "Audience of the identity token requested from GitHub Actions (default the endpoint's origin)")
	f.BoolVar(&exchangeExport, "export", false, "In GitHub Actions, set VALAR_TOKEN for the job's later steps instead of printing it")
	authCmd.AddCommand(authExchangeCmd)
}
