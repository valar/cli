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

var buildAbort bool

var buildCmd = &cobra.Command{
	Use:   "build [taskid]",
	Short: "List or inspect builds",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cfg := &config.Config{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			fmt.Println("Missing configuration, please run `valar init` first")
			return
		}
		client := api.NewClient(endpoint, token)
		if len(args) == 0 {
			buildListAll(client, cfg)
		} else {
			buildListOne(client, cfg, args[0])
		}
	},
}

func buildListAll(client *api.Client, cfg *config.Config) {
	tasks, err := client.ListBuilds(cfg.Project, cfg.Function)
	if err != nil {
		fmt.Println(err)
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

func buildListOne(client *api.Client, cfg *config.Config, id string) {
	task, err := client.GetBuild(cfg.Project, cfg.Function, id)
	if err != nil {
		fmt.Println(err)
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
	fmt.Fprintln(tw, "ID:\t", task.ID)
	fmt.Fprintln(tw, "Label:\t", task.Label)
	fmt.Fprintln(tw, "Created:\t", task.Created)
	fmt.Fprintln(tw, "Status:\t", task.Status)
	fmt.Fprintln(tw, "Domain:\t", task.Domain)
	if task.Err != nil {
		fmt.Fprintln(tw, "Err:\t", task.Err)
	}
	tw.Flush()
	if task.Logs != "" {
		fmt.Println("Logs:")
		fmt.Println(task.Logs)
	}
}

func init() {
	buildCmd.PersistentFlags().BoolVarP(&buildAbort, "abort", "a", false, "abort the build")
	rootCmd.AddCommand(buildCmd)
}
