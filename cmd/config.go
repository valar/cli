package cmd

import (
	"fmt"
	"os"

	"github.com/juju/ansiterm"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configure the CLI tool",
}

var configViewCmd = &cobra.Command{
	Use:   "view",
	Short: "View the merged configuration as YAML",
	Run: func(cmd *cobra.Command, args []string) {
		enc := yaml.NewEncoder(os.Stdout)
		enc.SetIndent(2)
		enc.Encode(globalConfiguration)
	},
}

var configEndpointCmd = &cobra.Command{
	Use:   "endpoint",
	Short: "Manage API endpoints",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		tw := ansiterm.NewTabWriter(os.Stdout, 6, 0, 1, ' ', 0)
		fmt.Fprintln(tw, "NAME\tURL\tTOKEN")
		for name, ep := range globalConfiguration.Endpoints {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", name, ep.URL, ep.Token)
		}
		tw.Flush()
	},
}

var configEndpointSetUrl, configEndpointSetToken string

var configEndpointSetCmd = &cobra.Command{
	Use:   "set [endpoint]",
	Short: "Configure an API endpoint",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ep := globalConfiguration.Endpoints[args[0]]
		if configEndpointSetToken != "" {
			ep.Token = configEndpointSetToken
		}
		if configEndpointSetUrl != "" {
			ep.URL = configEndpointSetUrl
		}
		if globalConfiguration.Endpoints == nil {
			globalConfiguration.Endpoints = make(map[string]valarEndpoint)
		}
		globalConfiguration.Endpoints[args[0]] = ep
		if err := globalConfiguration.Write(); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		return nil
	},
}

var configEndpointRemoveCmd = &cobra.Command{
	Use:   "remove [endpoint]",
	Short: "Drop an API endpoint from the configuration",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		delete(globalConfiguration.Endpoints, args[0])
		if err := globalConfiguration.Write(); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		return nil
	},
}

var configContextCmd = &cobra.Command{
	Use:   "context",
	Short: "Manage CLI contexts",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		tw := ansiterm.NewTabWriter(os.Stdout, 6, 0, 1, ' ', 0)
		fmt.Fprintln(tw, "ACTIVE\tNAME\tENDPOINT\tPROJECT")
		for name, ctx := range globalConfiguration.Contexts {
			active := ""
			if name == globalConfiguration.ActiveContext {
				active = "*"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", active, name, ctx.Endpoint, ctx.Project)
		}
		tw.Flush()
	},
}

var configContextUseCmd = &cobra.Command{
	Use:   "use [context]",
	Short: "Change the active context",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		globalConfiguration.ActiveContext = args[0]
		if err := globalConfiguration.Write(); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		return nil
	},
}

var configContextSetEndpoint, configContextSetProject string

var configContextSetCmd = &cobra.Command{
	Use:   "set [context]",
	Short: "Configure a CLI context",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := globalConfiguration.Contexts[args[0]]
		if configContextSetEndpoint != "" {
			ctx.Endpoint = configContextSetEndpoint
		}
		if configContextSetProject != "" {
			ctx.Project = configContextSetProject
		}
		globalConfiguration.Contexts[args[0]] = ctx
		if err := globalConfiguration.Write(); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		return nil
	},
}

var configContextRemoveCmd = &cobra.Command{
	Use:   "remove [context]",
	Short: "Drop a context from the configuration",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		delete(globalConfiguration.Contexts, args[0])
		if err := globalConfiguration.Write(); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		return nil
	},
}

func initConfigCmd() {
	configEndpointSetCmd.Flags().StringVar(&configEndpointSetToken, "token", "", "Token to use")
	configEndpointSetCmd.Flags().StringVar(&configEndpointSetUrl, "url", "", "URL the API can be reached on")
	configEndpointCmd.AddCommand(configEndpointSetCmd)
	configEndpointCmd.AddCommand(configEndpointRemoveCmd)
	configContextSetCmd.Flags().StringVar(&configContextSetProject, "project", "", "Project to use")
	configContextSetCmd.Flags().StringVar(&configContextSetEndpoint, "endpoint", "", "API endpoint")
	configContextCmd.AddCommand(configContextUseCmd)
	configContextCmd.AddCommand(configContextSetCmd)
	configContextCmd.AddCommand(configContextRemoveCmd)
	configCmd.AddCommand(configContextCmd)
	configCmd.AddCommand(configEndpointCmd)
	configCmd.AddCommand(configViewCmd)
	rootCmd.AddCommand(configCmd)
}
