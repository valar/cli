package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/valar/cli/api"
	"github.com/valar/cli/config"
)

var (
	cronService string

	cronCmd = &cobra.Command{
		Use:   "cron",
		Short: "Manage scheduled invocations of a service.",
	}

	cronListCmd = &cobra.Command{
		Use:   "list",
		Short: "List all cron schedules for a service.",
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &cronService, globalConfiguration)
			if err != nil {
				return err
			}
			client, err := globalConfiguration.APIClient()
			if err != nil {
				return err
			}
			schedules, err := client.ListSchedules(cfg.Project(), cfg.Service())
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
			fmt.Fprintln(tw, "NAME\tTIMESPEC\tPATH\tSTATUS")
			for _, sched := range schedules {
				fmt.Fprintln(tw, strings.Join([]string{sched.Name, sched.Timespec, sched.Path, colorize(sched.Status)}, "\t"))
			}
			tw.Flush()
			return nil
		}),
	}

	cronSetPayload       string
	cronSetPath          string
	cronSetEnabledCount  int
	cronSetDisabledCount int
	cronSetCmd           = &cobra.Command{
		Use:   "set [--payload payload] [--path path] [--enable|--disable] name [timespec]",
		Short: "Set a service invocation schedule.",
		Args:  cobra.RangeArgs(1, 2),
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &cronService, globalConfiguration)
			if err != nil {
				return err
			}
			client, err := globalConfiguration.APIClient()
			if err != nil {
				return err
			}
			// Attempt to fetch the schedule already present.
			var apiError api.Error
			schedule := api.Schedule{}
			existing, err := client.InspectSchedule(cfg.Project(), cfg.Service(), args[0])
			if err != nil && !(errors.As(err, &apiError) && apiError.StatusCode == http.StatusNotFound) {
				return err
			}
			if existing != nil {
				schedule = *existing.Schedule
				if len(args) == 2 {
					schedule.Timespec = args[1]
				}
				if cmd.Flags().Changed("payload") {
					schedule.Payload = cronSetPayload
				}
				if cmd.Flags().Changed("path") {
					schedule.Path = cronSetPath
				}
				if cmd.Flags().Changed("enabled") || cmd.Flags().Changed("disabled") {
					if cronSetEnabledCount-cronSetDisabledCount >= 0 {
						schedule.Status = "enabled"
					} else {
						schedule.Status = "disabled"
					}
				}
			} else {
				if len(args) != 2 {
					fmt.Fprintln(os.Stderr, "Timespec must be specified when setting a schedule for the first time.")
					os.Exit(1)
				}
				schedule = api.Schedule{
					Name:     args[0],
					Timespec: args[1],
					Path:     cronSetPath,
					Payload:  cronSetPayload,
				}
				if cronSetEnabledCount-cronSetDisabledCount >= 0 {
					schedule.Status = "enabled"
				} else {
					schedule.Status = "disabled"
				}
			}
			if err := client.SetSchedule(cfg.Project(), cfg.Service(), schedule); err != nil {
				return err
			}
			return nil
		}),
	}

	cronTriggerCmd = &cobra.Command{
		Use:   "trigger schedule",
		Short: "Manually triggers a scheduled invocation.",
		Args:  cobra.ExactArgs(1),
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &cronService, globalConfiguration)
			if err != nil {
				return err
			}
			client, err := globalConfiguration.APIClient()
			if err != nil {
				return err
			}
			if err := client.TriggerSchedule(cfg.Project(), cfg.Service(), args[0]); err != nil {
				return err
			}
			return nil
		}),
	}
	cronDeleteCmd = &cobra.Command{
		Use:   "delete schedule",
		Short: "Delete a service invocation schedule.",
		Args:  cobra.ExactArgs(1),
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &cronService, globalConfiguration)
			if err != nil {
				return err
			}
			client, err := globalConfiguration.APIClient()
			if err != nil {
				return err
			}
			if err := client.DeleteSchedule(cfg.Project(), cfg.Service(), args[0]); err != nil {
				return err
			}
			return nil
		}),
	}
	cronInspectCmd = &cobra.Command{
		Use:   "inspect schedule",
		Short: "Inspect the details of a service schedule.",
		Args:  cobra.ExactArgs(1),
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &cronService, globalConfiguration)
			if err != nil {
				return err
			}
			client, err := globalConfiguration.APIClient()
			if err != nil {
				return err
			}
			details, err := client.InspectSchedule(cfg.Project(), cfg.Service(), args[0])
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
			fmt.Fprintln(tw, "Name:\t", details.Schedule.Name)
			fmt.Fprintln(tw, "Timespec:\t", details.Schedule.Timespec)
			fmt.Fprintln(tw, "Path:\t", details.Schedule.Path)
			fmt.Fprintln(tw, "Payload:\t", details.Schedule.Payload)
			fmt.Fprintln(tw, "Status:\t", colorize(details.Schedule.Status))
			if details.LastRun == nil {
				fmt.Fprintln(tw, "Last Run:\t", "-")
			} else {
				fmt.Fprintln(tw, "Last Run:\t", "")
				fmt.Fprintln(tw, "  Start:\t", details.LastRun.StartTime)
				fmt.Fprintln(tw, "  End:\t", details.LastRun.EndTime)
				fmt.Fprintln(tw, "  Status:\t", colorize(details.LastRun.Status))
			}
			tw.Flush()
			return nil
		}),
	}
)

func initCronCmd() {
	rootCmd.AddCommand(cronCmd)
	cronCmd.PersistentFlags().StringVarP(&cronService, "service", "s", "", "The service to manage cron schedules for")
	cronCmd.AddCommand(cronListCmd, cronSetCmd, cronTriggerCmd, cronDeleteCmd, cronInspectCmd)
	cronSetCmd.Flags().StringVar(&cronSetPath, "path", "/", "The service path to send a request to")
	cronSetCmd.Flags().StringVar(&cronSetPayload, "payload", "", "The body payload to send in a request")
	cronSetCmd.Flags().CountVar(&cronSetEnabledCount, "enabled", "Enables the specified schedule")
	cronSetCmd.Flags().CountVar(&cronSetDisabledCount, "disabled", "Disables the specified schedule to prevent it from triggering")
}
