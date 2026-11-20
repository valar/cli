package cmd

import (
	"fmt"
	"io"
	"time"

	"github.com/valar/cli/api"
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
			return fmt.Errorf("deployment of version %d failed%s", deployment.Version, detail(deployment.Error))
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
