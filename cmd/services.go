package cmd

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
	"github.com/valar/cli/api"
	"github.com/valar/cli/config"
)

const functionConfiguration = ".valar.yml"

var initIgnore []string
var initProject, initConstructor string
var initForce bool

var serviceCmd = &cobra.Command{
	Use:     "service",
	Short:   "Manage individual services.",
	Aliases: []string{"svc", "services"},
}

var serviceInitCmd = &cobra.Command{
	Use:   "init service",
	Short: "Configure a new service.",
	Args:  cobra.ExactArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		if initProject == "" {
			initProject = globalConfiguration.Project()
		}
		if err := api.VerifyNames(initProject, args[0]); err != nil {
			return fmt.Errorf("bad naming scheme: %w", err)
		}
		cfg := &config.ServiceConfigYAML{
			Project: initProject,
			Service: args[0],
			Build: &config.BuildConfig{
				Constructor: initConstructor,
				Ignore:      initIgnore,
			},
			Deployment: &config.DeploymentConfig{},
		}
		if _, err := os.Stat(functionConfiguration); err == nil && !initForce {
			return fmt.Errorf("configuration already exists, please use --force flag to override")
		}
		return cfg.WriteToFile(functionConfiguration)
	}),
}

var serviceListCmd = &cobra.Command{
	Use:   "list [prefix]",
	Short: "Show services in the current project.",
	Args:  cobra.MaximumNArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		serviceCfg, err := config.NewServiceConfigWithFallback(functionConfiguration, nil, globalConfiguration)
		if err != nil {
			return err
		}
		prefix := ""
		if len(args) == 1 {
			prefix = args[0]
		}
		services, err := client.ListServices(serviceCfg.Project(), prefix)
		if err != nil {
			return fmt.Errorf("listing services: %w", err)
		}
		sort.Slice(services, func(i, j int) bool {
			return services[i].DeployedAt.Before(services[j].DeployedAt)
		})
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
		fmt.Fprintln(tw, "NAME\tVERSION\tCREATED\tLAST DEPLOYED\tDOMAINS")
		for _, svc := range services {
			fmt.Fprintln(tw, strings.Join([]string{
				svc.Name,
				strconv.FormatInt(svc.Deployment, 10),
				humanize.Time(svc.CreatedAt),
				humanize.Time(svc.DeployedAt),
				strings.Join(svc.Domains, " "),
			}, "\t"))
		}
		tw.Flush()
		return nil
	}),
}

var (
	serviceLogsFollow  bool
	serviceLogsTail    bool
	serviceLogsLines   int
	serviceLogsService string
)

var serviceLogsCmd = &cobra.Command{
	Use:   "logs [--service service]",
	Short: "Show the logs of the latest deployment.",
	Args:  cobra.NoArgs,
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &serviceLogsService, globalConfiguration)
		if err != nil {
			return err
		}
		return client.StreamServiceLogs(cfg.Project(), cfg.Service(), os.Stdout, serviceLogsFollow, serviceLogsTail, serviceLogsLines)
	}),
}

var serviceDisableService string

var serviceDisableCmd = &cobra.Command{
	Use:   "disable [--service service]",
	Short: "Disables the service.",
	Args:  cobra.NoArgs,
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &serviceDisableService, globalConfiguration)
		if err != nil {
			return err
		}
		return client.ChangeServiceStatus(cfg.Project(), cfg.Service(), false)
	}),
}

var serviceEnableService string

var serviceEnableCmd = &cobra.Command{
	Use:   "enable [--service service]",
	Short: "Enables the service.",
	Args:  cobra.NoArgs,
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &serviceEnableService, globalConfiguration)
		if err != nil {
			return err
		}
		return client.ChangeServiceStatus(cfg.Project(), cfg.Service(), true)
	}),
}

func initServicesCmd() {
	initPf := serviceInitCmd.PersistentFlags()
	initPf.StringArrayVarP(&initIgnore, "ignore", "i", []string{".valar.yml", ".git", "node_modules"}, "Ignore files on push")
	initPf.StringVarP(&initConstructor, "type", "t", "", "Build constructor type")
	initPf.StringVarP(&initProject, "project", "p", "", "Project to deploy service to, defaults to project set in global config")
	initPf.BoolVarP(&initForce, "force", "f", false, "Allow configuration override")
	cobra.MarkFlagRequired(initPf, "type")
	serviceLogsCmd.Flags().BoolVarP(&serviceLogsFollow, "follow", "f", false, "Follow logs")
	serviceLogsCmd.Flags().BoolVarP(&serviceLogsTail, "tail", "t", false, "Jump to end of logs")
	serviceLogsCmd.Flags().IntVarP(&serviceLogsLines, "skip", "n", 0, "Lines to skip/rewind when reading logs")
	serviceLogsCmd.Flags().StringVarP(&serviceLogsService, "service", "s", "", "The service to target")
	serviceCmd.AddCommand(serviceListCmd, serviceLogsCmd, serviceInitCmd, serviceEnableCmd, serviceDisableCmd)
	rootCmd.AddCommand(serviceCmd)
}
