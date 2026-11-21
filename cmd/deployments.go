package cmd

import (
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/juju/ansiterm"
	"github.com/spf13/cobra"
	"github.com/valar/cli/api"
	"github.com/valar/cli/config"
)

var deploymentService string

var deploymentCmd = &cobra.Command{
	Use:     "deployment",
	Short:   "Manage the deployments of a service.",
	Aliases: []string{"deploys", "deploy", "d"},
}

var deploymentListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List deployments of the service.",
	Aliases: []string{"l"},
	Args:    cobra.NoArgs,
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &deploymentService, globalConfiguration)
		if err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		return listDeployments(client, cfg)
	}),
}

var deploymentCreateCmd = &cobra.Command{
	Use:   "create [build]",
	Short: "Deploy the build with the fully given ID.",
	Long: `Deploy the build with the fully given ID.

Without a build, redeploy the build of the latest active deployment with the
service's current runtime variables, for example after 'valar env set'.`,
	Args: cobra.MaximumNArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &deploymentService, globalConfiguration)
		if err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		build := ""
		if len(args) > 0 {
			build = args[0]
		}
		return deployBuild(client, cfg, build, stdout)
	}),
}

var (
	rollbackDelta    int
	rollbackEnv      string
	rollbackSettings string
)

var deploymentRollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Reverse service to the previous deployment that served.",
	Long: `Reverse service to the previous deployment that served.

The target is the newest deployment below the live one that is done or
expired and has no failure; failed deployments are skipped. With --delta n it
is the n-th such deployment. The live deployment is the newest done one.

With --env pinned, the default, the new deployment gets the environment the
earlier one ran with; the command then names the variables whose values differ
from the current ones. With --env current, it gets the current variables.

With --settings pinned, the platform's default, the new deployment gets the
settings the earlier one ran with; with --settings current, the service's
current settings.`,
	Args: cobra.NoArgs,
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &deploymentService, globalConfiguration)
		if err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		return rollbackLatestDeployment(client, cfg, rollbackOptions{rollbackDelta, rollbackEnv, rollbackSettings}, stdout, os.Stderr)
	}),
}

type rollbackOptions struct {
	Delta    int
	Env      string
	Settings string
}

// served reports whether a deployment went live and did not fail. The API
// does not tell an expired deployment that served from one replaced before it
// went live, so both count.
func served(d api.Deployment) bool {
	return (d.Status == "done" || d.Status == "expired") && d.Failure == nil && d.Error == ""
}

// rollbackTarget returns the delta-th deployment below the newest done one
// that served.
func rollbackTarget(deployments []api.Deployment, delta int) (int64, error) {
	if delta < 1 {
		return 0, newError(kindInvalid, "--delta must be at least 1")
	}
	sorted := slices.Clone(deployments)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Version > sorted[j].Version })
	live := slices.IndexFunc(sorted, func(d api.Deployment) bool { return d.Status == "done" })
	if live < 0 {
		return 0, newError(kindNotFound, "no deployment is live to roll back from")
	}
	found := 0
	for _, d := range sorted[live+1:] {
		if served(d) {
			found++
			if found == delta {
				return d.Version, nil
			}
		}
	}
	return 0, newError(kindNotFound, "found %d earlier deployments that served below version %d, --delta %d needs more", found, sorted[live].Version, delta)
}

// rollbackLatestDeployment prints the new version to stdout and notes to stderr.
func rollbackLatestDeployment(client *api.Client, cfg config.ServiceConfig, opts rollbackOptions, stdout, stderr io.Writer) error {
	switch opts.Env {
	case api.RollbackEnvPinned:
	case api.RollbackEnvCurrent:
		// A server without the environment API ignores the field and rolls
		// back with the pinned environment.
		ok, err := client.HasEnvAPI(cfg.Project(), cfg.Service())
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("--env current needs Valar platform %s or newer with the environment API enabled", api.EnvMinPlatform)
		}
	default:
		return newError(kindInvalid, "unknown --env %q, want %q or %q", opts.Env, api.RollbackEnvPinned, api.RollbackEnvCurrent)
	}
	switch opts.Settings {
	case "", api.RollbackEnvPinned, api.RollbackEnvCurrent:
	default:
		return newError(kindInvalid, "unknown --settings %q, want %q or %q", opts.Settings, api.RollbackEnvPinned, api.RollbackEnvCurrent)
	}
	deployments, err := client.ListDeployments(cfg.Project(), cfg.Service())
	if err != nil {
		return err
	}
	target, err := rollbackTarget(deployments, opts.Delta)
	if err != nil {
		return err
	}
	result, err := client.RollbackDeploy(cfg.Project(), cfg.Service(), &api.RollbackRequest{
		Version:     target,
		Environment: opts.Env,
		Settings:    opts.Settings,
	})
	if err != nil {
		return settingsRefusal(err)
	}
	if err := emit(stdout, result, func(w io.Writer) error {
		_, err := fmt.Fprintln(w, result.Version)
		return err
	}); err != nil {
		return err
	}
	if len(result.Changed) > 0 {
		fmt.Fprintf(stderr, "The environment of version %d differs from the current one in %s.\n", target, strings.Join(result.Changed, ", "))
	}
	return nil
}

func listDeployments(client *api.Client, cfg config.ServiceConfig) error {
	deployments, err := client.ListDeployments(cfg.Project(), cfg.Service())
	if err != nil {
		return err
	}
	// Sort by version
	sort.Slice(deployments, func(i, j int) bool {
		return deployments[i].Version < deployments[j].Version
	})
	return result(deployments, func(w io.Writer) error {
		tw := ansiterm.NewTabWriter(w, 6, 0, 1, ' ', 0)
		fmt.Fprintln(tw, "VERSION\tSTATUS\tCREATED\tBUILD\tERROR")
		for _, d := range deployments {
			status, msg := colorize(d.Status), d.Error
			if d.Failure != nil {
				status += " (" + escapeControl(d.Failure.Kind) + ")"
				msg = d.Failure.Message
			}
			fmt.Fprintln(tw, strings.Join([]string{
				strconv.FormatInt(d.Version, 10),
				status,
				humanize.Time(d.CreatedAt),
				d.Build,
				escapeControl(msg),
			}, "\t"))
		}
		tw.Flush()
		return nil
	})
}

func initDeploymentsCmd() {
	deploymentRollbackCmd.Flags().IntVarP(&rollbackDelta, "delta", "d", 1, "Roll back to the n-th earlier deployment that served")
	deploymentRollbackCmd.Flags().StringVar(&rollbackEnv, "env", api.RollbackEnvPinned, "Environment of the new deployment (pinned|current)")
	deploymentRollbackCmd.Flags().StringVar(&rollbackSettings, "settings", "", "Settings of the new deployment (pinned|current); pinned unless given")
	deploymentCmd.PersistentFlags().StringVarP(&deploymentService, "service", "s", "", "The service to manage")
	deploymentCmd.AddCommand(deploymentListCmd, deploymentRollbackCmd, deploymentCreateCmd)
	rootCmd.AddCommand(deploymentCmd)
}
