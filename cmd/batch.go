package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/valar/cli/api"
	"github.com/valar/cli/config"
)

var (
	batchService string

	batchCmd = &cobra.Command{
		Use:   "batch",
		Short: "Manage batch executions of a service.",
		Long: `Manage batch executions of a service.

Batch services run containers to completion instead of serving traffic. Create
one by pushing a build with --kind batch, then trigger executions on demand.`,
	}

	batchRunBuild  string
	batchRunEnv    []string
	batchRunFollow bool
	batchRunWait   bool
	batchRunCmd    = &cobra.Command{
		Use:   "run [-- args...]",
		Short: "Trigger a new batch execution.",
		Long: `Trigger a new batch execution.

Arguments after -- replace the image's default command. Without --build the
latest successful build is used.`,
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			cfg, client, err := batchContext()
			if err != nil {
				return err
			}
			env, err := parseBatchEnv(batchRunEnv)
			if err != nil {
				return err
			}
			exec, err := client.TriggerBatchExecution(cfg.Project(), cfg.Service(), &api.BatchExecutionRequest{
				Build:       batchRunBuild,
				Args:        args,
				Environment: env,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Execution %s %s\n", exec.ID, colorize(exec.Status))
			if !batchRunFollow && !batchRunWait {
				return nil
			}
			final, err := awaitBatchCompletion(client, cfg, exec.ID)
			if err != nil {
				return err
			}
			if batchRunFollow {
				// Deliberately fetched after completion rather than streamed
				// live. Batch containers routinely finish in under a second, so
				// a tail started on "running" usually attaches to a log that is
				// already closed and the stream ends before anything arrives.
				// Waiting first makes the output deterministic.
				if err := client.StreamBatchExecutionLogs(cfg.Project(), cfg.Service(), exec.ID, os.Stdout, false); err != nil {
					fmt.Fprintf(os.Stderr, "could not read execution output: %v\n", err)
				}
			}
			fmt.Printf("Execution %s %s (exit %d)\n", final.ID, colorize(final.Status), final.ExitCode)
			if final.Status != "succeeded" {
				if final.Error != "" {
					fmt.Fprintln(os.Stderr, final.Error)
				}
				os.Exit(1)
			}
			return nil
		}),
	}

	batchListCmd = &cobra.Command{
		Use:   "list",
		Short: "List batch executions of a service.",
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			cfg, client, err := batchContext()
			if err != nil {
				return err
			}
			execs, err := client.ListBatchExecutions(cfg.Project(), cfg.Service())
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
			fmt.Fprintln(tw, "ID\tSTATUS\tEXIT\tBUILD\tCREATED\tNOTE")
			for _, exec := range execs {
				fmt.Fprintln(tw, strings.Join([]string{
					exec.ID,
					colorize(exec.Status),
					strconv.Itoa(int(exec.ExitCode)),
					shortID(exec.Build),
					exec.CreatedAt.Local().Format(time.RFC822),
					batchNote(exec),
				}, "\t"))
			}
			return tw.Flush()
		}),
	}

	batchInspectCmd = &cobra.Command{
		Use:   "inspect execution",
		Short: "Show the details of a batch execution.",
		Args:  cobra.ExactArgs(1),
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			cfg, client, err := batchContext()
			if err != nil {
				return err
			}
			exec, err := client.InspectBatchExecution(cfg.Project(), cfg.Service(), args[0])
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "ID:\t%s\n", exec.ID)
			fmt.Fprintf(tw, "Status:\t%s\n", colorize(exec.Status))
			fmt.Fprintf(tw, "Exit code:\t%d\n", exec.ExitCode)
			fmt.Fprintf(tw, "Build:\t%s\n", exec.Build)
			fmt.Fprintf(tw, "Owner:\t%s\n", exec.Owner)
			fmt.Fprintf(tw, "Attempts:\t%d\n", exec.Attempts)
			fmt.Fprintf(tw, "Created:\t%s\n", exec.CreatedAt.Local().Format(time.RFC1123))
			if exec.FinishedAt != nil {
				fmt.Fprintf(tw, "Finished:\t%s\n", exec.FinishedAt.Local().Format(time.RFC1123))
			}
			if len(exec.Args) > 0 {
				fmt.Fprintf(tw, "Args:\t%s\n", strings.Join(exec.Args, " "))
			}
			for key, value := range exec.Environment {
				fmt.Fprintf(tw, "Env:\t%s=%s\n", key, value)
			}
			if exec.BlockedGate != "" {
				fmt.Fprintf(tw, "Blocked by:\t%s\n", batchGateReason(exec.BlockedGate))
			}
			if exec.Error != "" {
				fmt.Fprintf(tw, "Error:\t%s\n", exec.Error)
			}
			return tw.Flush()
		}),
	}

	batchLogsFollow bool
	batchLogsCmd    = &cobra.Command{
		Use:   "logs execution",
		Short: "Show the container output of a batch execution.",
		Args:  cobra.ExactArgs(1),
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			cfg, client, err := batchContext()
			if err != nil {
				return err
			}
			return client.StreamBatchExecutionLogs(cfg.Project(), cfg.Service(), args[0], os.Stdout, batchLogsFollow)
		}),
	}

	batchCancelCmd = &cobra.Command{
		Use:   "cancel execution",
		Short: "Cancel a pending or running batch execution.",
		Args:  cobra.ExactArgs(1),
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			cfg, client, err := batchContext()
			if err != nil {
				return err
			}
			if err := client.CancelBatchExecution(cfg.Project(), cfg.Service(), args[0]); err != nil {
				return err
			}
			fmt.Printf("Execution %s cancelled\n", args[0])
			return nil
		}),
	}

	batchConfigParallelism int32
	batchConfigTimeout     int32
	batchConfigWeight      int32
	batchConfigCmd         = &cobra.Command{
		Use:   "config [--max-parallelism n] [--timeout seconds] [--weight n]",
		Short: "Show or change the batch configuration of a service.",
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			cfg, client, err := batchContext()
			if err != nil {
				return err
			}
			current, err := client.GetBatchConfig(cfg.Project(), cfg.Service())
			if err != nil {
				return err
			}
			// A PATCH with no flags would silently rewrite the config with the
			// values it just read, so only write when something was asked for.
			changed := false
			for name, target := range map[string]*int32{
				"max-parallelism": &current.MaxParallelism,
				"timeout":         &current.TimeoutSeconds,
				"weight":          &current.Weight,
			} {
				if cmd.Flags().Changed(name) {
					switch name {
					case "max-parallelism":
						*target = batchConfigParallelism
					case "timeout":
						*target = batchConfigTimeout
					case "weight":
						*target = batchConfigWeight
					}
					changed = true
				}
			}
			if changed {
				if current, err = client.SetBatchConfig(cfg.Project(), cfg.Service(), current); err != nil {
					return err
				}
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "Max parallelism:\t%s\n", zeroAsDefault(current.MaxParallelism, "1"))
			fmt.Fprintf(tw, "Timeout:\t%s\n", zeroAsDefault(current.TimeoutSeconds, "3600 (platform default)"))
			fmt.Fprintf(tw, "Weight:\t%s\n", zeroAsDefault(current.Weight, "1 (platform default)"))
			return tw.Flush()
		}),
	}
)

// batchContext resolves the service config and an API client together, since
// every batch subcommand needs both.
func batchContext() (config.ServiceConfig, *api.Client, error) {
	cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, &batchService, globalConfiguration)
	if err != nil {
		return nil, nil, err
	}
	client, err := globalConfiguration.APIClient()
	if err != nil {
		return nil, nil, err
	}
	return cfg, client, nil
}

func parseBatchEnv(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	env := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		key, value, found := strings.Cut(pair, "=")
		if !found || key == "" {
			return nil, fmt.Errorf("invalid environment entry %q, want KEY=VALUE", pair)
		}
		env[key] = value
	}
	return env, nil
}

// batchGateReason translates an admission gate into something actionable. The
// four situations look identical from the outside but have different remedies.
func batchGateReason(gate string) string {
	switch gate {
	case "follower_fit":
		return "no single runner has room for this execution (capacity is fragmented)"
	case "overcommit":
		return "would exceed a runner's memory overcommit ceiling"
	case "batch_budget":
		return "batch work is at its configured share of the cluster"
	case "dispatch_window":
		return "too many batch jobs already queued on the scheduler"
	case "stale_view":
		return "the control plane has lost sight of the cluster"
	default:
		return gate
	}
}

func batchNote(exec api.BatchExecution) string {
	if exec.BlockedGate != "" {
		return batchGateReason(exec.BlockedGate)
	}
	return exec.Error
}

func zeroAsDefault(value int32, fallback string) string {
	if value == 0 {
		return fallback
	}
	return strconv.Itoa(int(value))
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

const batchPollInterval = 2 * time.Second

func awaitBatchCompletion(client *api.Client, cfg config.ServiceConfig, id string) (*api.BatchExecution, error) {
	for {
		exec, err := client.InspectBatchExecution(cfg.Project(), cfg.Service(), id)
		if err != nil {
			return nil, err
		}
		switch exec.Status {
		case "succeeded", "failed", "cancelled":
			return exec, nil
		}
		time.Sleep(batchPollInterval)
	}
}

func initBatchCmd() {
	batchCmd.PersistentFlags().StringVarP(&batchService, "service", "s", "", "The batch service to operate on")

	batchRunCmd.Flags().StringVarP(&batchRunBuild, "build", "b", "", "Build to run (default: latest successful)")
	batchRunCmd.Flags().StringArrayVarP(&batchRunEnv, "env", "e", nil, "Environment variable as KEY=VALUE, repeatable")
	batchRunCmd.Flags().BoolVarP(&batchRunFollow, "follow", "f", false, "Wait for the execution to finish, then print its output")
	batchRunCmd.Flags().BoolVarP(&batchRunWait, "wait", "w", false, "Wait for the execution to finish and exit non-zero if it failed")
	batchLogsCmd.Flags().BoolVarP(&batchLogsFollow, "follow", "f", false, "Follow the output")

	batchConfigCmd.Flags().Int32Var(&batchConfigParallelism, "max-parallelism", 0, "Maximum concurrent executions")
	batchConfigCmd.Flags().Int32Var(&batchConfigTimeout, "timeout", 0, "Maximum seconds a single execution may run")
	batchConfigCmd.Flags().Int32Var(&batchConfigWeight, "weight", 0, "Relative share of batch capacity for this project")

	batchCmd.AddCommand(batchRunCmd, batchListCmd, batchInspectCmd, batchLogsCmd, batchCancelCmd, batchConfigCmd)
	rootCmd.AddCommand(batchCmd)
}
