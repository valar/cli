package cmd

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/valar/cli/api"
	"golang.org/x/term"
)

// pollInterval is how often waitForPush asks for build and deployment status.
var pollInterval = 2 * time.Second

// waitForPush streams the build log of buildID to w and waits for the build
// and, when deploy is set, for the deployment it started. It fails when
// either fails or the timeout passes.
func waitForPush(client *api.Client, project, service, buildID string, deploy bool, timeout time.Duration, w io.Writer) error {
	deadline := time.Now().Add(timeout)
	if err := client.StreamBuildLogs(project, service, buildID, func(le api.LogEntry) {
		fmt.Fprintln(w, formatLogEntry(&le, 0))
	}); err != nil {
		fmt.Fprintf(w, "Build log stream ended early: %v\n", err)
	}
	build, err := waitForBuild(client, project, service, buildID, deadline)
	if err != nil {
		return err
	}
	switch build.Status {
	case "done":
		fmt.Fprintln(w, "Build has succeeded.")
	case "failed":
		return fmt.Errorf("build %s failed%s", buildID, detail(build.Err))
	default:
		return fmt.Errorf("build %s ended as %s", buildID, build.Status)
	}
	if !deploy {
		return nil
	}
	return waitForDeployment(client, project, service, buildID, deadline, w)
}

func waitForBuild(client *api.Client, project, service, buildID string, deadline time.Time) (*api.Build, error) {
	for {
		build, err := client.InspectBuild(project, service, buildID)
		if err != nil {
			return nil, err
		}
		switch build.Status {
		case "done", "failed", "expired":
			return build, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for build %s, which is %s", buildID, build.Status)
		}
		time.Sleep(pollInterval)
	}
}

// waitForDeployment waits for the newest deployment of buildID to become
// active. A batch service has none, which is not an error.
func waitForDeployment(client *api.Client, project, service, buildID string, deadline time.Time, w io.Writer) error {
	for {
		deployments, err := client.ListDeployments(project, service)
		if err != nil {
			return err
		}
		var deployment *api.Deployment
		for i := range deployments {
			if deployments[i].Build == buildID && (deployment == nil || deployments[i].Version > deployment.Version) {
				deployment = &deployments[i]
			}
		}
		if deployment == nil {
			batch, err := isBatchService(client, project, service)
			if err != nil {
				return err
			}
			if batch {
				fmt.Fprintln(w, "Batch services are not deployed; the build runs on demand.")
				return nil
			}
			return fmt.Errorf("no deployment was created for build %s", buildID)
		}
		switch deployment.Status {
		case "done":
			fmt.Fprintf(w, "Deployed version %d.\n", deployment.Version)
			return nil
		case "expired":
			fmt.Fprintf(w, "Version %d was replaced by a newer deployment.\n", deployment.Version)
			return nil
		case "failed":
			return deploymentFailed(client, project, service, deployment, deployments, w)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for version %d, which is %s", deployment.Version, deployment.Status)
		}
		time.Sleep(pollInterval)
	}
}

func isBatchService(client *api.Client, project, service string) (bool, error) {
	services, err := client.ListServices(project, service)
	if err != nil {
		return false, err
	}
	for _, svc := range services {
		if svc.Name == service {
			return svc.IsBatch(), nil
		}
	}
	return false, fmt.Errorf("service %s not found", service)
}

func detail(msg string) string {
	if msg == "" {
		return ""
	}
	return ": " + msg
}

// failureLogLines is how many lines of a failed deployment's log a terminal
// shows.
const failureLogLines = 20

// stderrIsTerminal reports whether stderr is a terminal; tests replace it.
var stderrIsTerminal = func() bool { return term.IsTerminal(int(os.Stderr.Fd())) }

// deploymentFailed prints the end of a failed deployment's log on an
// interactive terminal and returns why it failed with a hint to its log.
func deploymentFailed(client *api.Client, project, service string, failed *api.Deployment, deployments []api.Deployment, w io.Writer) error {
	ci := os.Getenv("CI")
	if !outputJSON && (ci == "" || ci == "false") && stderrIsTerminal() {
		printLogTail(client, project, service, failed.Version, w)
	}
	return newError(kindFailed, "%s\nRead its log with: valar service logs --deployment %d", failureMessage(failed, deployments), failed.Version)
}

// failureMessage describes a failed deployment, with its failure kind and the
// version still serving when the server classifies failures.
func failureMessage(failed *api.Deployment, deployments []api.Deployment) string {
	msg := fmt.Sprintf("deployment of version %d failed", failed.Version)
	if failed.Failure == nil {
		return msg + detail(escapeControl(failed.Error))
	}
	msg += fmt.Sprintf(" (%s)%s", escapeControl(failed.Failure.Kind), detail(escapeControl(failed.Failure.Message)))
	var serving int64
	for _, d := range deployments {
		if d.Status == "done" && d.Version > serving {
			serving = d.Version
		}
	}
	if serving > 0 {
		msg += fmt.Sprintf("; version %d keeps serving", serving)
	}
	return msg
}

// printLogTail prints the last lines of a deployment's log. It is best
// effort: the log may be missing or unreadable.
func printLogTail(client *api.Client, project, service string, version int64, w io.Writer) {
	var lines []string
	opts := api.LogOptions{Deployment: int(version), Tail: true, Skip: failureLogLines}
	err := client.StreamServiceLogRecords(project, service, opts, func(record api.LogRecord) error {
		lines = append(lines, escapeControl(record.Line))
		return nil
	})
	if err != nil || len(lines) == 0 {
		return
	}
	fmt.Fprintf(w, "Last lines of version %d:\n", version)
	for _, line := range lines[max(0, len(lines)-failureLogLines):] {
		fmt.Fprintln(w, "  "+line)
	}
}

// escapeControl escapes control characters other than tabs, such as terminal
// escape sequences, so that server text cannot drive the terminal.
func escapeControl(s string) string {
	if strings.IndexFunc(s, isEscaped) < 0 {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if isEscaped(r) {
			q := strconv.QuoteRune(r)
			b.WriteString(q[1 : len(q)-1])
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isEscaped(r rune) bool { return r != '\t' && unicode.IsControl(r) }
