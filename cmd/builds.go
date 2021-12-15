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

var buildCmd = &cobra.Command{
	Use:   "builds [prefix]",
	Short: "List builds of the service",
	Args:  cobra.MaximumNArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		if len(args) < 1 {
			args = append(args, "")
		}
		return listBuilds(client, cfg, args[0])
	}),
}

var buildAbortCmd = &cobra.Command{
	Use:   "abort [prefix]",
	Short: "Abort a scheduled or running build",
	Args:  cobra.MaximumNArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		if len(args) < 1 {
			args = append(args, "")
		}
		return client.AbortBuild(cfg.Project, cfg.Service, args[0])
	}),
}

var logsFollow = false

var buildLogsCmd = &cobra.Command{
	Use:   "logs [task]",
	Short: "Show the build logs of the given task",
	Args:  cobra.MaximumNArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		// Get latest matching build
		prefix := ""
		if len(args) > 0 {
			prefix = args[0]
		}
		builds, err := client.ListBuilds(cfg.Project, cfg.Service, prefix)
		if err != nil {
			return err
		}
		if len(builds) == 0 {
			return fmt.Errorf("no builds available")
		}
		// Sort builds by date
		sort.Slice(builds, func(i, j int) bool { return builds[i].CreatedAt.After(builds[j].CreatedAt) })
		latestBuildID := builds[0].ID
		if logsFollow {
			return client.StreamBuildLogs(cfg.Project, cfg.Service, latestBuildID, os.Stdout)
		}
		return client.ShowBuildLogs(cfg.Project, cfg.Service, latestBuildID, os.Stdout)
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
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		return inspectBuild(client, cfg, args[0])
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
	var deployReq api.DeployRequest
	deployReq.Build = id
	for _, kv := range cfg.Deployment.Environment {
		deployReq.Environment = append(deployReq.Environment, api.KVPair(kv))
	}
	deployment, err := client.SubmitDeploy(cfg.Project, cfg.Service, &deployReq)
	if err != nil {
		return err
	}
	fmt.Println(deployment.Version)
	return nil
}

func colorize(status string) string {
	switch status {
	case "scheduled", "waiting":
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

func initBuildsCmd() {
	buildLogsCmd.PersistentFlags().BoolVarP(&logsFollow, "follow", "f", false, "follow the logs")
	buildCmd.AddCommand(inspectCmd)
	buildCmd.AddCommand(buildLogsCmd)
	buildCmd.AddCommand(buildAbortCmd)
	rootCmd.AddCommand(buildCmd)
}
