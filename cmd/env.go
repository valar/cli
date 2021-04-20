package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"valar/cli/pkg/api"
	"valar/cli/pkg/config"

	"github.com/spf13/cobra"
)

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage environment variables",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			return err
		}
		client, err := api.NewClient(endpoint, token)
		if err != nil {
			return err
		}
		scope := "deployment"
		if envBuild {
			scope = "build"
		}
		kvs, err := client.ListEnvironment(cfg.Project, cfg.Service, scope)
		if err != nil {
			return err
		}
		switch envFormat {
		case "raw":
			for _, kv := range kvs {
				fmt.Printf("%s=%s\n", kv.Key, kv.Value)
			}
		case "table":
			tw := tabwriter.NewWriter(os.Stdout, 0, 1, 1, ' ', 0)
			fmt.Fprintln(tw, "Key\tValue\tSecret")
			for _, kv := range kvs {
				fmt.Fprintf(tw, "%s\t%s\t%v\n", kv.Key, kv.Value, kv.Secret)
			}
			tw.Flush()
		default:
			return fmt.Errorf("unknown env format %s", envFormat)
		}
		return nil
	},
}

var envFormat string
var envBuild bool
var envSetSecret bool

var envSetCmd = &cobra.Command{
	Use:   "set [envvar]=[value]",
	Short: "Set variable to the given value",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			return err
		}
		client, err := api.NewClient(endpoint, token)
		if err != nil {
			return err
		}
		scope := "deployment"
		if envBuild {
			scope = "build"
		}
		// parse arg
		kvp := strings.SplitN(args[0], "=", 2)
		if len(kvp) != 2 {
			return fmt.Errorf("bad arg format, must be key=value")
		}
		if err := client.SetEnvironment(cfg.Project, cfg.Service, scope, kvp[0], kvp[1], envSetSecret); err != nil {
			return err
		}
		return nil
	},
}

var envDeleteCmd = &cobra.Command{
	Use:   "delete [variable]",
	Short: "Delete the environment variable",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := &config.ServiceConfig{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			return err
		}
		client, err := api.NewClient(endpoint, token)
		if err != nil {
			return err
		}
		scope := "deployment"
		if envBuild {
			scope = "build"
		}
		if err := client.DeleteEnvironment(cfg.Project, cfg.Service, scope, args[0]); err != nil {
			return err
		}
		return nil
	},
}

func initEnvCmd() {
	envSetCmd.Flags().BoolVar(&envSetSecret, "secret", false, "Hide variable content in logs and other listings")
	envCmd.PersistentFlags().BoolVarP(&envBuild, "build", "b", false, "Build scope instead of deployments")
	envCmd.Flags().StringVar(&envFormat, "format", "table", "Choose display format (table|raw)")
	envCmd.AddCommand(envSetCmd)
	envCmd.AddCommand(envDeleteCmd)
	rootCmd.AddCommand(envCmd)
}
