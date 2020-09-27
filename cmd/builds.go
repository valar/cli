package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"valar/cli/pkg/api"
	"valar/cli/pkg/config"

	"github.com/dustin/go-humanize"
	"github.com/fatih/color"
	"github.com/juju/ansiterm"
	"github.com/spf13/cobra"
)

var buildAbort bool

var buildCmd = &cobra.Command{
	Use:   "builds [prefix]",
	Short: "List builds of the service",
	Args:  cobra.MaximumNArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			return err
		}
		client, err := api.NewClient(endpoint, token)
		if err != nil {
			return err
		}
		if len(args) < 1 {
			args = append(args, "")
		}
		return listBuilds(client, cfg, args[0])
	}),
}

var logsFollow = false

var buildLogsCmd = &cobra.Command{
	Use:   "logs [task]",
	Short: "Show the build logs of the given task",
	Args:  cobra.ExactArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			return err
		}
		client, err := api.NewClient(endpoint, token)
		if err != nil {
			return err
		}
		if logsFollow {
			return client.StreamBuildLogs(cfg.Project, cfg.Service, args[0], os.Stdout)
		}
		return client.ShowBuildLogs(cfg.Project, cfg.Service, args[0], os.Stdout)
	}),
}

var inspectCmd = &cobra.Command{
	Use:   "inspect [prefix]",
	Short: "Inspect the first matched task with the given ID prefix",
	Args:  cobra.ExactArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			return err
		}
		client, err := api.NewClient(endpoint, token)
		if err != nil {
			return err
		}
		return inspectBuild(client, cfg, args[0])
	}),
}

var deployCmd = &cobra.Command{
	Use:   "deploy [id]",
	Short: "Deploy the build with the fully given ID",
	Args:  cobra.ExactArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			return err
		}
		client, err := api.NewClient(endpoint, token)
		if err != nil {
			return err
		}
		return deployBuild(client, cfg, args[0])
	}),
}

func listBuilds(client *api.Client, cfg *config.ServiceConfig, id string) error {
	builds, err := client.ListBuilds(cfg.Project, cfg.Service, id)
	if err != nil {
		return err
	}
	// Sort by date
	sort.Slice(builds, func(i, j int) bool {
		return builds[i].CreatedAt.Before(builds[j].CreatedAt)
	})
	tw := ansiterm.NewTabWriter(os.Stdout, 6, 0, 1, ' ', 0)
	fmt.Fprintln(tw, "ID\tStatus\tCreated")
	for _, b := range builds {
		fmt.Fprintln(tw, strings.Join([]string{
			b.ID,
			colorize(b.Status),
			humanize.Time(b.CreatedAt),
		}, "\t"))
	}
	tw.Flush()
	return nil
}

func inspectBuild(client *api.Client, cfg *config.ServiceConfig, id string) error {
	build, err := client.InspectBuild(cfg.Project, cfg.Service, id)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
	fmt.Fprintln(tw, "ID:\t", build.ID)
	fmt.Fprintln(tw, "Constructor:\t", build.Constructor)
	fmt.Fprintln(tw, "CreatedAt:\t", build.CreatedAt)
	fmt.Fprintln(tw, "Status:\t", colorize(build.Status))
	fmt.Fprintln(tw, "Flags:\t", build.Flags)
	fmt.Fprintln(tw, "Owner:\t", build.Owner)
	if build.Err != "" {
		fmt.Fprintln(tw, "Err:\t", build.Err)
	}
	tw.Flush()
	return nil
}

func deployBuild(client *api.Client, cfg *config.ServiceConfig, id string) error {
	deployment, err := client.SubmitDeploy(cfg.Project, cfg.Service, id)
	if err != nil {
		return err
	}
	fmt.Println(deployment.Version)
	return nil
}

func colorize(status string) string {
	switch status {
	case "scheduled":
		return color.HiYellowString("%s", status)
	case "building", "releasing", "binding":
		return color.YellowString("%s", status)
	case "done":
		return color.GreenString("%s", status)
	case "failed":
		return color.RedString("%s", status)
	default:
		return status
	}
}

func init() {
	buildCmd.PersistentFlags().BoolVarP(&buildAbort, "abort", "a", false, "abort the build")
	buildLogsCmd.PersistentFlags().BoolVarP(&logsFollow, "follow", "f", false, "follow the logs")
	buildCmd.AddCommand(inspectCmd)
	buildCmd.AddCommand(buildLogsCmd)
	buildCmd.AddCommand(deployCmd)
	rootCmd.AddCommand(buildCmd)
}
