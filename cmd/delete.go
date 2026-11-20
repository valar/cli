package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/valar/cli/api"
	"golang.org/x/term"
)

// confirmation reads a name to confirm a deletion. A nil confirmation skips
// the question, as --yes does.
type confirmation struct {
	in       io.Reader
	terminal bool
}

func stdinConfirmation(yes bool) *confirmation {
	if yes {
		return nil
	}
	return &confirmation{in: os.Stdin, terminal: term.IsTerminal(int(os.Stdin.Fd()))}
}

// ask prints warning and asks for the name of the subject being deleted.
// Without a terminal it refuses, so a script never blocks on the question.
func (c *confirmation) ask(w io.Writer, subject, name, warning string) error {
	if c == nil {
		return nil
	}
	if !c.terminal {
		return newError(kindInvalid, "refusing to delete %s %s without confirmation: standard input is not a terminal, pass --yes", subject, name)
	}
	fmt.Fprintln(w, warning)
	fmt.Fprintf(w, "Type the %s name to confirm: ", subject)
	line, _ := bufio.NewReader(c.in).ReadString('\n')
	if strings.TrimSpace(line) != name {
		return newError(kindInvalid, "aborted: the input does not match %s, pass --yes to skip the confirmation", name)
	}
	return nil
}

// deleteRetryDelays are the waits before each retry of a deletion.
var deleteRetryDelays = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}

// retryDeletion calls del until it succeeds, retrying a 409 or 5xx answer.
// A 404 on a retry means an earlier attempt finished the deletion.
func retryDeletion(w io.Writer, del func() error) error {
	for attempt := 0; ; attempt++ {
		err := del()
		var apiErr api.Error
		if err == nil || !errors.As(err, &apiErr) {
			return err
		}
		if attempt > 0 && apiErr.StatusCode == http.StatusNotFound {
			return nil
		}
		retryable := apiErr.StatusCode == http.StatusConflict || apiErr.StatusCode >= 500
		if !retryable || attempt == len(deleteRetryDelays) {
			return err
		}
		delay := deleteRetryDelays[attempt]
		fmt.Fprintf(w, "Deletion not finished (%v), retrying in %s ...\n", err, delay)
		time.Sleep(delay)
	}
}

func deleteService(client *api.Client, project, service string, confirm *confirmation, w io.Writer) error {
	if err := api.VerifyNames(project, service); err != nil {
		return withKind(kindInvalid, err)
	}
	warning := fmt.Sprintf("Deleting service %s in project %s removes its builds, deployments, batch executions, environment, secrets and schedules.", service, project)
	if err := confirm.ask(w, "service", service, warning); err != nil {
		return err
	}
	if err := retryDeletion(w, func() error { return client.DeleteService(project, service) }); err != nil {
		return fmt.Errorf("delete service %s: %w", service, err)
	}
	fmt.Fprintf(w, "Deleted service %s.\n", service)
	return nil
}
