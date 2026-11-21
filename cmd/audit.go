package cmd

import (
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/juju/ansiterm"
	"github.com/spf13/cobra"
	"github.com/valar/cli/api"
	"github.com/valar/cli/config"
)

const defaultAuditLimit = 100

var (
	auditProject string
	auditLimit   int
	auditBefore  int64
)

var auditCmd = &cobra.Command{
	Use:   "audit [--project p] [--limit n] [--before id]",
	Short: "Show a project's audit log, newest first.",
	Long: `Show a project's audit log, newest first.

The log lists the changes made to the project through the API, each with the
token, client and source address that made it: an attempt row before the
change and a result row after it. Count rows sum a minute's refused requests
and requests by legacy project tokens.

It reads the active project unless --project names another, and needs
permission to manage the project. A page holds --limit rows, at most 500;
--before continues below the last ID of the previous page. This needs a newer
Valar platform.`,
	Args: cobra.NoArgs,
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		project := auditProject
		if project == "" {
			cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, nil, globalConfiguration)
			if err != nil {
				return err
			}
			project = cfg.Project()
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		return listAudit(client, project, auditBefore, auditLimit, stdout, os.Stderr)
	}),
}

// listAudit writes a page of the project's audit log to w and, when the page
// is full, the command for the next page to progress.
func listAudit(client *api.Client, project string, before int64, limit int, w, progress io.Writer) error {
	if project == "" {
		return newError(kindInvalid, "no project: pass --project, set VALAR_PROJECT or run in a folder with %s", functionConfiguration)
	}
	if err := api.VerifyProjectName(project); err != nil {
		return withKind(kindInvalid, err)
	}
	if limit < 1 || limit > api.MaxAuditPage {
		return newError(kindInvalid, "--limit must be between 1 and %d", api.MaxAuditPage)
	}
	if before < 0 {
		return newError(kindInvalid, "--before must be an event ID")
	}
	events, err := client.ListAuditEvents(project, before, limit)
	if err != nil {
		return err
	}
	return emit(w, events, func(w io.Writer) error {
		if err := printAudit(w, events); err != nil {
			return err
		}
		if len(events) == limit {
			next := fmt.Sprintf("valar audit --project %s --before %d", project, events[len(events)-1].ID)
			if limit != defaultAuditLimit {
				next += " --limit " + strconv.Itoa(limit)
			}
			fmt.Fprintf(progress, "Older rows: %s\n", next)
		}
		return nil
	})
}

func printAudit(w io.Writer, events []api.AuditEvent) error {
	tw := ansiterm.NewTabWriter(w, 6, 0, 1, ' ', 0)
	fmt.Fprintln(tw, "ID\tTIME\tKIND\tPRINCIPAL\tTOKEN\tOPERATION\tSERVICE\tTARGET\tSTATUS\tSOURCE\tCLIENT")
	for _, e := range events {
		kind := e.Kind
		if e.Kind == api.AuditCount {
			kind = fmt.Sprintf("count (%d)", e.Count)
		}
		token := e.TokenID
		if tokenIDPattern.MatchString(token) {
			token = token[:8]
		}
		status := "-"
		if e.Status != 0 {
			status = strconv.Itoa(e.Status)
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", e.ID, e.At.Local().Format("2006-01-02 15:04:05"), kind,
			e.Principal, orDash(token), e.Operation, orDash(e.Service), orDash(e.Target), status, orDash(e.SourceIP), orDash(e.Client))
	}
	return tw.Flush()
}

func initAuditCmd() {
	auditCmd.Flags().StringVar(&auditProject, "project", "", "Project to read (default the active project)")
	auditCmd.Flags().IntVar(&auditLimit, "limit", defaultAuditLimit, "Rows per page, at most 500")
	auditCmd.Flags().Int64Var(&auditBefore, "before", 0, "Show only rows with smaller IDs, to continue below the previous page")
	rootCmd.AddCommand(auditCmd)
}
