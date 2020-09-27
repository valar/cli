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
	Use:   "deployments",
	Short: "List deployments of the service",
	Args:  cobra.NoArgs,
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			return err
		}
		client, err := api.NewClient(endpoint, token)
		if err != nil {
			return err
		}
		return listDeployments(client, cfg)
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
		client, err := api.NewClient(endpoint, token)
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
	// Pick build from deployment 1
	build := deployments[rollbackDelta].Build
	// Deploy build
	rollback, err := client.SubmitDeploy(cfg.Project, cfg.Service, build)
	if err != nil {
		return err
	}
	fmt.Println(rollback.Status)
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
	fmt.Fprintln(tw, "Version\tStatus\tCreated\tBuild")
	for _, d := range deployments {
		fmt.Fprintln(tw, strings.Join([]string{
			strconv.FormatInt(d.Version, 10),
			colorize(d.Status),
			humanize.Time(d.CreatedAt),
			d.Build,
		}, "\t"))
	}
	tw.Flush()
	return nil
}

func init() {
	rollbackCmd.Flags().IntVarP(&rollbackDelta, "delta", "d", 1, "number of deployments to roll back")
	deploymentCmd.AddCommand(rollbackCmd)
	rootCmd.AddCommand(deploymentCmd)
}
