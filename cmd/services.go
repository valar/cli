package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"valar/cli/pkg/api"
	"valar/cli/pkg/config"
	"valar/cli/pkg/util"

	"github.com/spf13/cobra"
)

const functionConfiguration = ".valar.yml"

var initIgnore []string
var initEnv, initProject string
var initForce bool

var initCmd = &cobra.Command{
	Use:   "init [service]",
	Short: "Create a service configuration file",
	Args:  cobra.ExactArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		if err := api.VerifyNames(initProject, args[0]); err != nil {
			return fmt.Errorf("bad naming scheme: %w", err)
		}
		cfg := &config.Config{
			Ignore:   initIgnore,
			Project:  initProject,
			Function: args[0],
		}
		cfg.Environment.Name = initEnv
		if _, err := os.Stat(functionConfiguration); err == nil && !initForce {
			return fmt.Errorf("configuration already exists, please use --force flag to override")
		}
		return cfg.WriteToFile(functionConfiguration)
	}),
}

var listCmd = &cobra.Command{
	Use:   "list [prefix]",
	Short: "Show all services in the current project with the prefix",
	Args:  cobra.MaximumNArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg := &config.Config{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			return err
		}
		client := api.NewClient(endpoint, token)
		prefix := ""
		if len(args) == 1 {
			prefix = args[0]
		}
		services, err := client.ListServices(cfg.Project, prefix)
		if err != nil {
			return fmt.Errorf("listing services: %w", err)
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
		fmt.Fprintln(tw, "Label\tVersion\tTask")
		for _, svc := range services {
			fmt.Fprintln(tw, strings.Join([]string{
				svc.Label,
				svc.Version,
				svc.ID,
			}, "\t"))
		}
		tw.Flush()
		return nil
	}),
}

var serviceLogsCmd = &cobra.Command{
	Use:   "logs [service]",
	Short: "Show the logs of the latest service version",
	Args:  cobra.MaximumNArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg := &config.Config{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			return err
		}
		client := api.NewClient(endpoint, token)
		if len(args) == 1 {
			cfg.Function = args[0]
		}
		if logsFollow {
			return client.StreamServiceLogs(cfg.Project, cfg.Function, os.Stdout)
		}
		return client.ShowServiceLogs(cfg.Project, cfg.Function, os.Stdout)
	}),
}

func pushFolder(cfg *config.Config, folder string) (*api.Task, error) {
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
	client := api.NewClient(endpoint, token)
	task, err := client.SubmitBuild(cfg.Project, cfg.Function, cfg.Environment.Name, targzFile)
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
		cfg := &config.Config{}
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
	initPf.StringArrayVarP(&initIgnore, "ignore", "i", []string{".valar.yml", ".git", "node_modules"}, "ignore files")
	initPf.StringVarP(&initEnv, "type", "t", "", "build constructor type")
	initPf.StringVarP(&initProject, "project", "p", "", "project name")
	initPf.BoolVarP(&initForce, "force", "f", false, "allow configuration override")
	cobra.MarkFlagRequired(initPf, "type")
	cobra.MarkFlagRequired(initPf, "project")
	serviceLogsCmd.PersistentFlags().BoolVarP(&logsFollow, "follow", "f", false, "follow logs")

	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(serviceLogsCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(pushCmd)
}
