package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
	"github.com/valar/cli/api"
	"github.com/valar/cli/config"
)

var (
	cronService string

	cronCmd = &cobra.Command{
		Use:   "cron",
		Short: "Manage scheduled invocations of a service",
	}

	cronListCmd = &cobra.Command{
		Use:   "list",
		Short: "List all cron schedules for a service",
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
			fmt.Fprintln(tw, "NAME\tTIMESPEC\tPATH\tPAYLOAD")
			for _, sched := range schedules {
				fmt.Fprintln(tw, strings.Join([]string{sched.Name, sched.Timespec, sched.Path, sched.Payload}, "\t"))
			}
			tw.Flush()
			return nil
		}),
	}

	cronAddPayload string
	cronAddPath    string
	cronAddCmd     = &cobra.Command{
		Use:   "add [--payload payload] [--path path] name timespec",
		Short: "Add or edit a service invocation schedule",
		Args:  cobra.ExactArgs(2),
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &cronService, globalConfiguration)
			if err != nil {
				return err
			}
			client, err := globalConfiguration.APIClient()
			if err != nil {
				return err
			}
			if err := client.AddSchedule(cfg.Project(), cfg.Service(), api.Schedule{
				Name:     args[0],
				Timespec: args[1],
				Payload:  cronAddPayload,
				Path:     cronAddPath,
			}); err != nil {
				return err
			}
			return nil
		}),
	}

	cronTriggerCmd = &cobra.Command{
		Use:   "trigger schedule",
		Short: "Manually triggers a scheduled invocation",
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
	cronRemoveCmd = &cobra.Command{
		Use:   "remove schedule",
		Short: "Remove a service invocation schedule",
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
			if err := client.RemoveSchedule(cfg.Project(), cfg.Service(), args[0]); err != nil {
				return err
			}
			return nil
		}),
	}
	cronInspectCmd = &cobra.Command{
		Use:   "inspect schedule",
		Short: "Inspect the invocation history of a service schedule",
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
			invocations, err := client.InspectSchedule(cfg.Project(), cfg.Service(), args[0])
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
			fmt.Fprintln(tw, "ID\tSTATUS\tSTARTED\tENDED")
			for _, inv := range invocations {
				startTimeStr := humanize.Time(inv.StartTime)
				if inv.StartTime.IsZero() {
					startTimeStr = "-"
				}
				endTimeStr := humanize.Time(inv.EndTime)
				if inv.EndTime.IsZero() {
					endTimeStr = "-"
				}
				fmt.Fprintln(tw, strings.Join(
					[]string{
						inv.ID,
						colorize(inv.Status),
						startTimeStr,
						endTimeStr,
					}, "\t"))
			}
			tw.Flush()
			return nil
		}),
	}
)

func initCronCmd() {
	rootCmd.AddCommand(cronCmd)
	cronCmd.PersistentFlags().StringVarP(&cronService, "service", "s", "", "The service to manage cron schedules for")
	cronCmd.AddCommand(cronListCmd, cronAddCmd, cronTriggerCmd, cronRemoveCmd, cronInspectCmd)
	cronAddCmd.Flags().StringVar(&cronAddPath, "path", "/", "The service path to send a request to")
	cronAddCmd.Flags().StringVar(&cronAddPayload, "payload", "", "The body payload to send in a request")
}
