package cmd

import (
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/juju/ansiterm"
	"github.com/spf13/cobra"
	"github.com/valar/cli/api"
	"github.com/valar/cli/config"
)

// tokenExpiryWarning matches when the platform starts sending api.TokenExpiresHeader.
const tokenExpiryWarning = 14 * 24 * time.Hour

// expiryNotice warns about the token's expiry once per invocation.
type expiryNotice struct {
	w    io.Writer
	now  func() time.Time
	once sync.Once
}

func (n *expiryNotice) observe(endpoint string, expires time.Time) {
	left := expires.Sub(n.now())
	if left > tokenExpiryWarning {
		return
	}
	n.once.Do(func() {
		fmt.Fprintf(n.w, "Warning: your Valar token expires %s, at %s. %s\n",
			untilExpiry(left), expires.UTC().Format("2006-01-02 15:04 MST"), renewHint(endpoint))
	})
}

// untilExpiry rounds d to days, hours or minutes.
func untilExpiry(d time.Duration) string {
	unit, size := "minute", time.Minute
	switch {
	case d < time.Minute:
		return "within a minute"
	case d >= 48*time.Hour:
		unit, size = "day", 24*time.Hour
	case d >= 2*time.Hour:
		unit, size = "hour", time.Hour
	}
	n := int((d + size/2) / size)
	if n != 1 {
		unit += "s"
	}
	return fmt.Sprintf("in %d %s", n, unit)
}

// renewHint says where to create a replacement token and how to use it.
func renewHint(endpoint string) string {
	where := "in the Valar console"
	if consoleKnown(endpoint) {
		where = "at https://console.valar.dev/api-tokens/new"
	}
	return "Create a new token " + where + " and use it with 'valar config endpoint set <endpoint> --token <token>' or VALAR_TOKEN."
}

func consoleKnown(endpoint string) bool {
	u, err := url.Parse(endpoint)
	def, _ := url.Parse(config.DefaultEndpoint)
	return err == nil && u.Host == def.Host
}

var (
	tokenProject string
	tokenService string
	tokenScopes  []string
	tokenTTL     string
)

var tokenIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

var (
	tokenCmd = &cobra.Command{
		Use:     "token",
		Short:   "Create, list and revoke API tokens.",
		Aliases: []string{"tokens"},
		Long: `Create, list and revoke API tokens.

A token created here acts as you. It is a child of the token that creates it:
it expires no later than that token and is revoked with it. Only a token
created in the console or with authctl, with full access and no project
binding, can create tokens. These commands need a newer Valar platform.`,
	}

	tokenCreateCmd = &cobra.Command{
		Use:   "create [--project p] [--service s] [--scopes read,deploy] [--ttl 90d] name",
		Short: "Create a token and print it once.",
		Long: `Create a token that acts as you, and print it once.

--project binds the token to one project and --service to one service of it.
--scopes limits what it may do: read, deploy, invoke, kv or all, the default.
A token for CI that pushes and deploys one project needs only read and deploy:

  valar token create ci --project acme --scopes read,deploy

--ttl is the token's lifetime in days, such as 30d, or as a duration, such as
12h. It defaults to 90 days and is at most 365 days, and the token expires no
later than the token that creates it.

The token goes to stdout and everything else to stderr, so
TOKEN=$(valar token create ci) captures only the token. Valar shows it this
once: store it right away.`,
		Args: cobra.ExactArgs(1),
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			req, ttl, err := newTokenRequest(args[0], tokenProject, tokenService, tokenScopes, tokenTTL)
			if err != nil {
				return err
			}
			client, err := globalConfiguration.APIClient()
			if err != nil {
				return err
			}
			return createToken(client, req, ttl, stdout, os.Stderr, time.Now())
		}),
	}

	tokenListCmd = &cobra.Command{
		Use:   "list [--project p]",
		Short: "List your tokens, or the tokens bound to a project.",
		Long: `List your tokens, or with --project the tokens bound to that project.

Revoked and expired tokens are listed for 30 days. A project's list shows
every token bound to it, whoever owns it, and the tokens of its services, and
needs permission to manage the project.`,
		Args: cobra.NoArgs,
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			client, err := globalConfiguration.APIClient()
			if err != nil {
				return err
			}
			return listTokens(client, tokenProject, stdout, time.Now())
		}),
	}

	tokenRevokeCmd = &cobra.Command{
		Use:   "revoke [--project p] id",
		Short: "Revoke a token and the tokens it created.",
		Long: `Revoke a token and the tokens it created.

The ID is the one 'valar token list' shows. With --project, it revokes a token
from the project's list, which needs permission to manage the project.`,
		Args: cobra.ExactArgs(1),
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			client, err := globalConfiguration.APIClient()
			if err != nil {
				return err
			}
			return revokeToken(client, tokenProject, args[0], stdout)
		}),
	}
)

// parseTTL reads a lifetime in days, such as 90d, or as a Go duration, such as 12h.
func parseTTL(s string) (time.Duration, error) {
	bad := fmt.Errorf("--ttl %q is not a lifetime such as 90d or 12h", s)
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.ParseInt(days, 10, 64)
		if err != nil || n <= 0 || n > math.MaxInt64/int64(24*time.Hour) {
			return 0, bad
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, bad
	}
	return d, nil
}

// ttlString writes d as the Go duration the API takes, in hours where it can.
func ttlString(d time.Duration) string {
	if d%time.Hour == 0 {
		return strconv.FormatInt(int64(d/time.Hour), 10) + "h"
	}
	return d.String()
}

// newTokenRequest checks the flags of token create and returns the request
// and the lifetime it asks for, zero for the server's default.
func newTokenRequest(name, project, service string, scopes []string, ttl string) (api.TokenRequest, time.Duration, error) {
	req := api.TokenRequest{Name: name, Project: project, Service: service, Scopes: scopes}
	if service != "" && project == "" {
		return req, 0, newError(kindInvalid, "--service needs --project")
	}
	if ttl == "" {
		return req, 0, nil
	}
	d, err := parseTTL(ttl)
	if err != nil {
		return req, 0, withKind(kindInvalid, err)
	}
	req.TTL = ttlString(d)
	return req, d, nil
}

// createToken mints a token and writes its value alone to w, and what it
// grants to progress.
func createToken(client *api.Client, req api.TokenRequest, ttl time.Duration, w, progress io.Writer, now time.Time) error {
	token, err := client.CreateToken(req)
	if err != nil {
		return err
	}
	return emit(w, token, func(w io.Writer) error {
		fmt.Fprintf(progress, "Created token %s.\n", token.Name)
		tw := ansiterm.NewTabWriter(progress, 6, 0, 1, ' ', 0)
		writeRows(tw, tokenRows(token.Token, now))
		if err := tw.Flush(); err != nil {
			return err
		}
		if ttl > 0 && !token.CreatedAt.IsZero() && !token.ExpiresAt.IsZero() && token.ExpiresAt.Before(token.CreatedAt.Add(ttl-time.Minute)) {
			fmt.Fprintln(progress, "It expires with the token that created it, before the --ttl asked for.")
		}
		fmt.Fprintln(progress, "Store the token now; Valar shows it only once:")
		_, err := fmt.Fprintln(w, token.Value)
		return err
	})
}

func listTokens(client *api.Client, project string, w io.Writer, now time.Time) error {
	if project != "" {
		if err := api.VerifyProjectName(project); err != nil {
			return withKind(kindInvalid, err)
		}
	}
	tokens, err := client.ListTokens(project)
	if err != nil {
		return err
	}
	return emit(w, tokens, func(w io.Writer) error { return printTokens(w, tokens, project != "", now) })
}

// printTokens writes tokens as a table, naming each parent the list holds.
func printTokens(w io.Writer, tokens []api.Token, withOwner bool, now time.Time) error {
	names := make(map[string]string, len(tokens))
	for _, t := range tokens {
		names[t.ID] = t.Name
	}
	tw := ansiterm.NewTabWriter(w, 6, 0, 1, ' ', 0)
	owner := ""
	if withOwner {
		owner = "OWNER\t"
	}
	fmt.Fprintf(tw, "ID\tNAME\t%sKIND\tBINDING\tSCOPES\tPARENT\tEXPIRES\tLAST USED\tSTATUS\n", owner)
	for _, t := range tokens {
		if withOwner {
			owner = orDash(t.Owner) + "\t"
		}
		parent := t.Parent
		if name := names[parent]; name != "" {
			parent = name
		}
		bound := t.Project
		if t.Service != "" {
			bound += "/" + t.Service
		}
		fmt.Fprintf(tw, "%s\t%s\t%s%s\t%s\t%s\t%s\t%s\t%s\t%s\n", t.ID, orDash(t.Name), owner, t.Kind, orDash(bound),
			orDash(strings.Join(t.Scopes, ",")), orDash(parent), relative(t.ExpiresAt, now), relative(t.LastUsed, now), tokenStatus(t, now))
	}
	return tw.Flush()
}

func revokeToken(client *api.Client, project, id string, w io.Writer) error {
	if !tokenIDPattern.MatchString(id) {
		return newError(kindInvalid, "%q is not a token ID; 'valar token list' shows them", id)
	}
	if project != "" {
		if err := api.VerifyProjectName(project); err != nil {
			return withKind(kindInvalid, err)
		}
	}
	n, err := client.RevokeToken(project, id)
	if err != nil {
		return err
	}
	return emit(w, struct {
		ID      string `json:"id"`
		Revoked int    `json:"revoked"`
	}{id, n}, func(w io.Writer) error {
		var err error
		switch n {
		case 0:
			_, err = fmt.Fprintf(w, "Token %s was already revoked.\n", id)
		case 1:
			_, err = fmt.Fprintf(w, "Revoked token %s.\n", id)
		case 2:
			_, err = fmt.Fprintf(w, "Revoked token %s and 1 token it created.\n", id)
		default:
			_, err = fmt.Fprintf(w, "Revoked token %s and %d tokens it created.\n", id, n-1)
		}
		return err
	})
}

// tokenRows describes a token's grant as labelled rows, leaving out what the
// server did not report.
func tokenRows(t api.Token, now time.Time) [][2]string {
	var rows [][2]string
	if t.ID != "" {
		rows = append(rows, [2]string{"ID", t.ID})
	}
	scopes := orDash(strings.Join(t.Scopes, ", "))
	rows = append(rows, [2]string{"Kind", t.Kind}, [2]string{"Binding", binding(t)}, [2]string{"Scopes", scopes})
	if !t.CreatedAt.IsZero() {
		rows = append(rows, [2]string{"Created", stamp(t.CreatedAt, now)})
	}
	expires := "never"
	if !t.ExpiresAt.IsZero() {
		expires = stamp(t.ExpiresAt, now)
	}
	rows = append(rows, [2]string{"Expires", expires})
	if !t.LastUsed.IsZero() {
		rows = append(rows, [2]string{"Last used", stamp(t.LastUsed, now)})
	}
	if t.Parent != "" {
		rows = append(rows, [2]string{"Parent", t.Parent})
	}
	return rows
}

func writeRows(w io.Writer, rows [][2]string) {
	for _, row := range rows {
		fmt.Fprintf(w, "%s:\t%s\n", row[0], row[1])
	}
}

func binding(t api.Token) string {
	switch {
	case t.Service != "":
		return "service " + t.Project + "/" + t.Service
	case t.Project != "":
		return "project " + t.Project
	default:
		return "any project"
	}
}

func tokenStatus(t api.Token, now time.Time) string {
	switch {
	case !t.RevokedAt.IsZero():
		return "revoked"
	case !t.ExpiresAt.IsZero() && !now.Before(t.ExpiresAt):
		return "expired"
	default:
		return "live"
	}
}

// stamp writes t in UTC and relative to now.
func stamp(t, now time.Time) string {
	return t.UTC().Format("2006-01-02 15:04 MST") + " (" + humanize.RelTime(t, now, "ago", "from now") + ")"
}

func relative(t, now time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return humanize.RelTime(t, now, "ago", "from now")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func initTokenCmd() {
	tokenCreateCmd.Flags().StringVar(&tokenProject, "project", "", "Bind the token to this project")
	tokenCreateCmd.Flags().StringVar(&tokenService, "service", "", "Bind the token to this service of the project")
	tokenCreateCmd.Flags().StringSliceVar(&tokenScopes, "scopes", nil, "What the token may do: read, deploy, invoke, kv or all (default all)")
	tokenCreateCmd.Flags().StringVar(&tokenTTL, "ttl", "", "Lifetime, such as 30d or 12h (default 90d)")
	tokenListCmd.Flags().StringVar(&tokenProject, "project", "", "List the tokens bound to this project instead of yours")
	tokenRevokeCmd.Flags().StringVar(&tokenProject, "project", "", "Revoke a token from this project's list")
	tokenCmd.AddCommand(tokenCreateCmd, tokenListCmd, tokenRevokeCmd)
	rootCmd.AddCommand(tokenCmd)
}
