package cmd

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"valar/cli/pkg/api"
	"valar/cli/pkg/config"
	"valar/cli/pkg/util"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

const functionConfiguration = ".valar.yml"

var initIgnore []string
var initProject, initConstructor string
var initForce bool

var initCmd = &cobra.Command{
	Use:   "init [service]",
	Short: "Configure a new service",
	Args:  cobra.ExactArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		// TODO(lnsp): In case initProject is empty, fetch user name from server and use as project name
		if err := api.VerifyNames(initProject, args[0]); err != nil {
			return fmt.Errorf("bad naming scheme: %w", err)
		}
		cfg := &config.ServiceConfig{
			Ignore:      initIgnore,
			Project:     initProject,
			Service:     args[0],
			Constructor: initConstructor,
		}
		if _, err := os.Stat(functionConfiguration); err == nil && !initForce {
			return fmt.Errorf("configuration already exists, please use --force flag to override")
		}
		return cfg.WriteToFile(functionConfiguration)
	}),
}

var listCmd = &cobra.Command{
	Use:   "list [prefix]",
	Short: "Show services in the current project",
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
		prefix := ""
		if len(args) == 1 {
			prefix = args[0]
		}
		services, err := client.ListServices(cfg.Project, prefix)
		if err != nil {
			return fmt.Errorf("listing services: %w", err)
		}
		sort.Slice(services, func(i, j int) bool {
			return services[i].DeployedAt.Before(services[j].DeployedAt)
		})
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
		fmt.Fprintln(tw, "Name\tDeployment\tCreated\tLast deployed")
		for _, svc := range services {
			fmt.Fprintln(tw, strings.Join([]string{
				svc.Name,
				strconv.FormatInt(svc.Deployment, 10),
				humanize.Time(svc.CreatedAt),
				humanize.Time(svc.DeployedAt),
			}, "\t"))
		}
		tw.Flush()
		return nil
	}),
}

var serviceLogsCmd = &cobra.Command{
	Use:   "logs [service]",
	Short: "Show the logs of the latest deployment",
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
		if len(args) == 1 {
			cfg.Service = args[0]
		}
		if logsFollow {
			return client.StreamServiceLogs(cfg.Project, cfg.Service, os.Stdout)
		}
		return client.ShowServiceLogs(cfg.Project, cfg.Service, os.Stdout)
	}),
}

func pushFolder(cfg *config.ServiceConfig, folder string) (*api.Build, error) {
	archivePath, err := util.CompressDir(folder, cfg.Ignore)
	if err != nil {
		return nil, fmt.Errorf("package compression failed: %w", err)
	}
	defer os.Remove(archivePath)
	targzFile, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("package archive failed: %w", err)
	}
	defer targzFile.Close()
	client, err := api.NewClient(endpoint, token)
	if err != nil {
		return nil, err
	}
	task, err := client.SubmitBuild(cfg.Project, cfg.Service, cfg.Constructor, targzFile)
	if err != nil {
		return nil, err
	}
	return task, nil
}

var pushCmd = &cobra.Command{
	Use:   "push [folder]",
	Short: "Push a new version to Valar",
	Args:  cobra.MaximumNArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		// Load configuration
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			return err
		}
		folder, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("locating working directory: %w", err)
		}
		if len(args) != 0 {
			folder = args[0]
		}
		task, err := pushFolder(cfg, folder)
		if err != nil {
			return err
		}
		fmt.Println(task.ID)
		return nil
	}),
}

func init() {
	initPf := initCmd.PersistentFlags()
	initPf.StringArrayVarP(&initIgnore, "ignore", "i", []string{".valar.yml", ".git", "node_modules"}, "ignore files on push")
	initPf.StringVarP(&initConstructor, "type", "t", "", "build constructor type")
	initPf.StringVarP(&initProject, "project", "p", "", "project to deploy service to")
	initPf.BoolVarP(&initForce, "force", "f", false, "allow configuration override")
	cobra.MarkFlagRequired(initPf, "type")
	cobra.MarkFlagRequired(initPf, "project")
	serviceLogsCmd.PersistentFlags().BoolVarP(&logsFollow, "follow", "f", false, "follow logs")
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(serviceLogsCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(pushCmd)
}
