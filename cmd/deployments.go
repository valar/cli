package cmd

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"valar/cli/pkg/api"
	"valar/cli/pkg/config"

	"github.com/dustin/go-humanize"
	"github.com/juju/ansiterm"
	"github.com/spf13/cobra"
)

var deploymentCmd = &cobra.Command{
	Use:   "deploys",
	Short: "List deployments of the service",
	Args:  cobra.NoArgs,
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		return listDeployments(client, cfg)
	}),
}

var createCmd = &cobra.Command{
	Use:   "create [build]",
	Short: "Deploy the build with the fully given ID",
	Args:  cobra.ExactArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		return deployBuild(client, cfg, args[0])
	}),
}

var rollbackDelta int

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Reverse service to the previous deployment",
	Args:  cobra.NoArgs,
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		return rollbackLatestDeployment(client, cfg)
	}),
}

func rollbackLatestDeployment(client *api.Client, cfg *config.ServiceConfig) error {
	deployments, err := client.ListDeployments(cfg.Project, cfg.Service)
	if err != nil {
		return err
	}
	if len(deployments) < rollbackDelta+1 {
		return fmt.Errorf("not enough deployments available")
	}
	// Sort by version (desc)
	sort.Slice(deployments, func(i, j int) bool {
		return deployments[i].Version > deployments[j].Version
	})
	// Get targeted version identifier
	deployment, err := client.RollbackDeploy(cfg.Project, cfg.Service, &api.RollbackRequest{
		Version: deployments[rollbackDelta].Version,
	})
	if err != nil {
		return err
	}
	fmt.Println(deployment.Version)
	return nil
}

func listDeployments(client *api.Client, cfg *config.ServiceConfig) error {
	deployments, err := client.ListDeployments(cfg.Project, cfg.Service)
	if err != nil {
		return err
	}
	// Sort by version
	sort.Slice(deployments, func(i, j int) bool {
		return deployments[i].Version < deployments[j].Version
	})
	tw := ansiterm.NewTabWriter(os.Stdout, 6, 0, 1, ' ', 0)
	fmt.Fprintln(tw, "Version\tStatus\tCreated\tBuild\tError")
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
	rollbackCmd.Flags().IntVarP(&rollbackDelta, "delta", "d", 1, "number of deployments to roll back")
	deploymentCmd.AddCommand(rollbackCmd)
	deploymentCmd.AddCommand(createCmd)
	rootCmd.AddCommand(deploymentCmd)
}
