package cmd

import (
	"fmt"
	"io"
	"os"
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
		return deployBuild(client, cfg, build, os.Stdout)
	}),
}

var (
	rollbackDelta int
	rollbackEnv   string
)

var deploymentRollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Reverse service to the previous deployment.",
	Long: `Reverse service to the previous deployment.

With --env pinned, the default, the new deployment gets the environment the
earlier one ran with; the command then names the variables whose values differ
from the current ones. With --env current, it gets the current variables.`,
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
		return rollbackLatestDeployment(client, cfg, rollbackDelta, rollbackEnv, os.Stdout, os.Stderr)
	}),
}

// rollbackLatestDeployment prints the new version to stdout and notes to stderr.
func rollbackLatestDeployment(client *api.Client, cfg config.ServiceConfig, delta int, env string, stdout, stderr io.Writer) error {
	switch env {
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
		return fmt.Errorf("unknown --env %q, want %q or %q", env, api.RollbackEnvPinned, api.RollbackEnvCurrent)
	}
	deployments, err := client.ListDeployments(cfg.Project(), cfg.Service())
	if err != nil {
		return err
	}
	if len(deployments) < delta+1 {
		return fmt.Errorf("not enough deployments available")
	}
	// Sort by version (desc)
	sort.Slice(deployments, func(i, j int) bool {
		return deployments[i].Version > deployments[j].Version
	})
	target := deployments[delta].Version
	result, err := client.RollbackDeploy(cfg.Project(), cfg.Service(), &api.RollbackRequest{
		Version:     target,
		Environment: env,
	})
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, result.Version)
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
	tw := ansiterm.NewTabWriter(os.Stdout, 6, 0, 1, ' ', 0)
	fmt.Fprintln(tw, "VERSION\tSTATUS\tCREATED\tBUILD\tERROR")
	for _, d := range deployments {
		fmt.Fprintln(tw, strings.Join([]string{
			strconv.FormatInt(d.Version, 10),
			colorize(d.Status),
			humanize.Time(d.CreatedAt),
			d.Build,
			d.Error,
		}, "\t"))
	}
	tw.Flush()
	return nil
}

func initDeploymentsCmd() {
	deploymentRollbackCmd.Flags().IntVarP(&rollbackDelta, "delta", "d", 1, "Number of deployments to roll back")
	deploymentRollbackCmd.Flags().StringVar(&rollbackEnv, "env", api.RollbackEnvPinned, "Environment of the new deployment (pinned|current)")
	deploymentCmd.PersistentFlags().StringVarP(&deploymentService, "service", "s", "", "The service to manage")
	deploymentCmd.AddCommand(deploymentListCmd, deploymentRollbackCmd, deploymentCreateCmd)
	rootCmd.AddCommand(deploymentCmd)
}
