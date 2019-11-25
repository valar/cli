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
	"github.com/spf13/cobra"
)

var taskAbort bool

var taskCmd = &cobra.Command{
	Use:   "task [prefix]",
	Short: "List tasks with the given ID prefix",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cfg := &config.Config{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			fmt.Println("Missing configuration, please run `valar init` first")
			return
		}
		client := api.NewClient(endpoint, token)
		if len(args) < 1 {
			args = append(args, "")
		}
		listTasks(client, cfg, args[0])
	},
}

var logsFollow = false

var buildLogsCmd = &cobra.Command{
	Use:   "logs [task]",
	Short: "Show the build logs of the given task",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cfg := &config.Config{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			fmt.Println("Missing configuration, please run `valar init` first")
			return
		}
		client := api.NewClient(endpoint, token)
		if logsFollow {
			streamBuildLogs(client, cfg, args[0])
		} else {
			showBuildLogs(client, cfg, args[0])
		}
	},
}

var inspectCmd = &cobra.Command{
	Use:   "inspect [prefix]",
	Short: "Inspect the first matched task with the given ID prefix",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cfg := &config.Config{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			fmt.Println("Missing configuration, please run `valar init` first")
			return
		}
		client := api.NewClient(endpoint, token)
		inspectTask(client, cfg, args[0])
	},
}

func streamBuildLogs(client *api.Client, cfg *config.Config, id string) {
	if err := client.StreamTaskLogs(cfg.Project, cfg.Function, id, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "Streaming logs:", err)
		return
	}
}

func showBuildLogs(client *api.Client, cfg *config.Config, id string) {
	if err := client.ShowTaskLogs(cfg.Project, cfg.Function, id, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "Showing logs:", err)
		return
	}
}

func listTasks(client *api.Client, cfg *config.Config, id string) {
	tasks, err := client.ListTasks(cfg.Project, cfg.Function, id)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Listing tasks:", err)
		return
	}
	// Sort by date
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].Created.Before(tasks[j].Created)
	})
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
	fmt.Fprintln(tw, "ID\tStatus\tCreated")
	for _, t := range tasks {
		fmt.Fprintln(tw, strings.Join([]string{
			t.ID,
			t.Status,
			humanize.Time(t.Created),
		}, "\t"))
	}
	tw.Flush()
}

func inspectTask(client *api.Client, cfg *config.Config, id string) {
	task, err := client.InspectTask(cfg.Project, cfg.Function, id)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Retrieving build:", err)
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
	fmt.Fprintln(tw, "ID:\t", task.ID)
	fmt.Fprintln(tw, "Label:\t", task.Label)
	fmt.Fprintln(tw, "Created:\t", task.Created)
	fmt.Fprintln(tw, "Status:\t", task.Status)
	fmt.Fprintln(tw, "Domain:\t", task.Domain)
	if task.Err != "" {
		fmt.Fprintln(tw, "Err:\t", task.Err)
	}
	tw.Flush()
	if task.Logs != "" {
		fmt.Println("Logs:")
		fmt.Println(task.Logs)
	}
}

func init() {
	taskCmd.PersistentFlags().BoolVarP(&taskAbort, "abort", "a", false, "abort the build")
	buildLogsCmd.PersistentFlags().BoolVarP(&logsFollow, "follow", "f", false, "follow the logs")
	taskCmd.AddCommand(inspectCmd)
	taskCmd.AddCommand(buildLogsCmd)
	rootCmd.AddCommand(taskCmd)
}
