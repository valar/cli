package cmd

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

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
			return newError(kindInvalid, "bad naming scheme: %w", err)
		}
		if _, err := os.Stat(functionConfiguration); err == nil && !initForce {
			return newError(kindConflict, "configuration already exists, please use --force flag to override")
		}
		if initConstructor == "" {
			detected, err := detectConstructor(".", os.Stderr)
			if err != nil {
				return err
			}
			initConstructor = detected
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
		if err := cfg.WriteToFile(functionConfiguration); err != nil {
			return err
		}
		return result(cfg, func(io.Writer) error { return nil })
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
		return result(services, func(w io.Writer) error {
			tw := tabwriter.NewWriter(w, 0, 0, 1, ' ', 0)
			fmt.Fprintln(tw, "NAME\tKIND\tVERSION\tCREATED\tLAST DEPLOYED\tDOMAINS")
			for _, svc := range services {
				fmt.Fprintln(tw, strings.Join([]string{
					svc.Name,
					serviceKind(svc.Kind),
					deployedVersion(svc),
					humanize.Time(svc.CreatedAt),
					lastDeployed(svc),
					strings.Join(svc.Domains, " "),
				}, "\t"))
			}
			tw.Flush()
			return nil
		})
	}),
}

// serviceKind renders a service's workload type.
//
// Deliberately uncoloured. tabwriter measures cell width in bytes, so an ANSI
// sequence makes a cell look ~9 characters wider than it prints and the whole
// table skews -- which is already visible wherever colorize() is used inside a
// tabwriter elsewhere in this CLI. A column of its own distinguishes the kinds
// well enough without that cost.
func serviceKind(kind string) string {
	if kind == "" {
		// A server predating the kind field. Reporting "function" would be a
		// guess presented as fact.
		return "-"
	}
	return kind
}

// deployedVersion renders the deployment version, or a dash when there is no
// deployment to have a version.
func deployedVersion(svc api.Service) string {
	if !svc.Deployed() {
		return "-"
	}
	return strconv.FormatInt(svc.Deployment, 10)
}

// lastDeployed renders the deployment time, or a dash when the service has
// never been deployed.
//
// Humanising the zero timestamp yields "a long while ago", which reads as
// deployed in the distant past. For a batch service that is not merely
// imprecise: it is never deployed at all, by design.
func lastDeployed(svc api.Service) string {
	if !svc.Deployed() {
		return "-"
	}
	return humanize.Time(svc.DeployedAt)
}

var (
	serviceLogsOptions    api.LogOptions
	serviceLogsTimestamps bool
	serviceLogsService    string
)

var serviceLogsCmd = &cobra.Command{
	Use:   "logs [--service service]",
	Short: "Show the logs of the latest deployment, one deployment or all of them.",
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
		return showServiceLogs(client, cfg, serviceLogsOptions, serviceLogsTimestamps)
	}),
}

// showServiceLogs prints a service log: as plain text by default, as records
// with --json, or as lines prefixed with their time with timestamps.
func showServiceLogs(client *api.Client, cfg *config.ValidatedServiceConfig, opts api.LogOptions, timestamps bool) error {
	switch {
	case opts.Deployment < 0:
		return newError(kindInvalid, "--deployment must be a deployment version")
	case opts.All && (opts.Deployment > 0 || opts.Tail || opts.Skip > 0):
		return newError(kindInvalid, "--all cannot be combined with --deployment, --tail or --skip")
	}
	if !outputJSON && !timestamps {
		return client.StreamServiceLogs(cfg.Project(), cfg.Service(), stdout, opts)
	}
	return client.StreamServiceLogRecords(cfg.Project(), cfg.Service(), opts, func(record api.LogRecord) error {
		if outputJSON {
			return writeLogRecord(stdout, record)
		}
		line := record.Line
		if record.Stream == "platform" {
			line = "[platform] " + line
		}
		if record.TS != "" {
			line = record.TS + " " + line
		}
		_, err := fmt.Fprintln(stdout, line)
		return err
	})
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
		if err := client.ChangeServiceStatus(cfg.Project(), cfg.Service(), true); err != nil {
			return err
		}
		return result(serviceStatus{cfg.Project(), cfg.Service(), true}, func(io.Writer) error { return nil })
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
		if err := enableService(client, cfg.Project(), cfg.Service(), os.Stderr); err != nil {
			return err
		}
		return result(serviceStatus{cfg.Project(), cfg.Service(), false}, func(io.Writer) error { return nil })
	}),
}

type serviceStatus struct {
	Project  string `json:"project"`
	Service  string `json:"service"`
	Disabled bool   `json:"disabled"`
}

// enableRetryWindow bounds how long enabling retries while the platform still
// stops the instances of an earlier disable, which it answers with 409.
var enableRetryWindow = 2 * time.Minute

func enableService(client *api.Client, project, service string, w io.Writer) error {
	deadline := time.Now().Add(enableRetryWindow)
	waiting := false
	for {
		err := client.ChangeServiceStatus(project, service, false)
		var apiErr api.Error
		if err == nil || !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusConflict || time.Now().After(deadline) {
			return err
		}
		if !waiting {
			fmt.Fprintln(w, "Waiting for the instances of the earlier disable to stop ...")
			waiting = true
		}
		time.Sleep(pollInterval)
	}
}

var serviceDeleteYes bool

var serviceDeleteCmd = &cobra.Command{
	Use:   "delete [--yes] [service]",
	Short: "Delete a service and everything it holds.",
	Long: `Delete a service with its builds, deployments, batch executions,
environment, secrets, schedules and the permission rules on it. Its linked
domains are unlinked; the project keeps their claims.

The service defaults to the one in .valar.yml. The command asks for the
service name to confirm, unless --yes is given; without a terminal to ask on it
refuses instead. A deletion that fails leaves the service disabled, and running
the command again finishes it.`,
	Args: cobra.MaximumNArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		var service string
		if len(args) == 1 {
			service = args[0]
		}
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &service, globalConfiguration)
		if err != nil {
			return err
		}
		if err := deleteService(client, cfg.Project(), cfg.Service(), stdinConfirmation(serviceDeleteYes), os.Stderr); err != nil {
			return err
		}
		return result(struct {
			Service string `json:"service"`
			Deleted bool   `json:"deleted"`
		}{cfg.Service(), true}, func(io.Writer) error { return nil })
	}),
}

var serviceURLService string

var serviceURLCmd = &cobra.Command{
	Use:   "url [--service service]",
	Short: "Print the service's URLs, the preferred one first.",
	Args:  cobra.NoArgs,
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &serviceURLService, globalConfiguration)
		if err != nil {
			return err
		}
		project, service := cfg.Project(), cfg.Service()
		services, err := client.ListServices(project, service)
		if err != nil {
			return fmt.Errorf("listing services: %w", err)
		}
		for _, svc := range services {
			if svc.Name != service {
				continue
			}
			urls := serviceURLs(project, svc)
			if len(urls) == 0 {
				return newError(kindNotFound, "service %s has no domains yet; it gets them with its first deployment", service)
			}
			return result(struct {
				URLs []string `json:"urls"`
			}{urls}, func(w io.Writer) error {
				_, err := fmt.Fprintln(w, strings.Join(urls, "\n"))
				return err
			})
		}
		return newError(kindNotFound, "service %s not found", service)
	}),
}

// serviceURLs orders a service's domains as URLs: linked custom domains first,
// then the canonical <project>-<service>.<domain> ones, shortest first.
func serviceURLs(project string, svc api.Service) []string {
	canonicalPrefix := project + "-" + svc.Name + "."
	var custom, canonical []string
	for _, d := range svc.Domains {
		if strings.HasPrefix(d, canonicalPrefix) {
			canonical = append(canonical, d)
		} else {
			custom = append(custom, d)
		}
	}
	sort.Strings(custom)
	sort.Slice(canonical, func(i, j int) bool {
		if len(canonical[i]) != len(canonical[j]) {
			return len(canonical[i]) < len(canonical[j])
		}
		return canonical[i] < canonical[j]
	})
	urls := make([]string, 0, len(svc.Domains))
	for _, d := range append(custom, canonical...) {
		urls = append(urls, "https://"+d)
	}
	return urls
}

func initServicesCmd() {
	initPf := serviceInitCmd.PersistentFlags()
	initPf.StringArrayVarP(&initIgnore, "ignore", "i", []string{".valar.yml", ".git", "node_modules"}, "Ignore files on push")
	initPf.StringVarP(&initConstructor, "type", "t", "", "Build constructor type, detected from the project files when omitted")
	initPf.StringVarP(&initProject, "project", "p", "", "Project to deploy service to, defaults to project set in global config")
	initPf.BoolVarP(&initForce, "force", "f", false, "Allow configuration override")
	serviceLogsCmd.Flags().BoolVarP(&serviceLogsOptions.Follow, "follow", "f", false, "Follow logs")
	serviceLogsCmd.Flags().BoolVarP(&serviceLogsOptions.Tail, "tail", "t", false, "Jump to end of logs")
	serviceLogsCmd.Flags().IntVarP(&serviceLogsOptions.Skip, "skip", "n", 0, "Lines to skip/rewind when reading logs")
	serviceLogsCmd.Flags().BoolVar(&serviceLogsOptions.All, "all", false, "Show the logs of every deployment, oldest first")
	serviceLogsCmd.Flags().IntVar(&serviceLogsOptions.Deployment, "deployment", 0, "Show the log of this deployment version")
	serviceLogsCmd.Flags().BoolVar(&serviceLogsTimestamps, "timestamps", false, "Prefix each line with the time it was written")
	serviceLogsCmd.Flags().StringVarP(&serviceLogsService, "service", "s", "", "The service to target")
	serviceURLCmd.Flags().StringVarP(&serviceURLService, "service", "s", "", "The service to target")
	serviceDisableCmd.Flags().StringVarP(&serviceDisableService, "service", "s", "", "The service to target")
	serviceEnableCmd.Flags().StringVarP(&serviceEnableService, "service", "s", "", "The service to target")
	serviceDeleteCmd.Flags().BoolVarP(&serviceDeleteYes, "yes", "y", false, "Delete without asking for the service name")
	serviceCmd.AddCommand(serviceListCmd, serviceLogsCmd, serviceInitCmd, serviceEnableCmd, serviceDisableCmd, serviceURLCmd, serviceDeleteCmd)
	rootCmd.AddCommand(serviceCmd)
}
