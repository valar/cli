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
	Short: "List all deployments",
	Args:  cobra.NoArgs,
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg := &config.Config{}
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

func listDeployments(client *api.Client, cfg *config.Config) error {
	deployments, err := client.ListDeployments(cfg.Project, cfg.Service)
	if err != nil {
		return err
	}
	// Sort by version
	sort.Slice(deployments, func(i, j int) bool {
		return deployments[i].Version < deployments[j].Version
	})
	tw := ansiterm.NewTabWriter(os.Stdout, 6, 0, 1, ' ', 0)
	fmt.Fprintln(tw, "Version\tStatus\tCreated")
	for _, d := range deployments {
		fmt.Fprintln(tw, strings.Join([]string{
			strconv.FormatInt(d.Version, 10),
			colorize(d.Status),
			humanize.Time(d.CreatedAt),
		}, "\t"))
	}
	tw.Flush()
	return nil
}

func init() {
	rootCmd.AddCommand(deploymentCmd)
}
