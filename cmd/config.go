package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/juju/ansiterm"
	"github.com/spf13/cobra"
	"github.com/valar/cli/api"
	"github.com/valar/cli/config"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

func prompt(label, defaultValue string) string {
	if defaultValue != "" {
		fmt.Fprintf(os.Stderr, "%s (%s): ", label, defaultValue)
	} else {
		fmt.Fprintf(os.Stderr, "%s: ", label)
	}
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultValue
	}
	return line
}

func promptSecret(label string) string {
	fmt.Fprintf(os.Stderr, "%s: ", label)
	b, _ := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return string(b)
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configure the CLI tool",
}

var configViewCmd = &cobra.Command{
	Use:   "view",
	Short: "View the merged configuration as YAML.",
	Run: func(cmd *cobra.Command, args []string) {
		enc := yaml.NewEncoder(os.Stdout)
		enc.SetIndent(2)
		enc.Encode(globalConfiguration)
	},
}

var configEndpointCmd = &cobra.Command{
	Use:   "endpoint",
	Short: "Manage API endpoints.",
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
	Short: "Configure an API endpoint.",
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
			globalConfiguration.Endpoints = map[string]config.APIEndpoint{}
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
	Short: "Drop an API endpoint from the configuration.",
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
	Short: "Manage CLI contexts.",
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
	Short: "Change the active context.",
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
	Short: "Configure a CLI context.",
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
	Short: "Drop a context from the configuration.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		delete(globalConfiguration.Contexts, args[0])
		if err := globalConfiguration.Write(); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		return nil
	},
}

var configInitUrl, configInitToken, configInitProject, configInitName string
var configInitForce bool

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Set up a new CLI configuration interactively.",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Collect values from flags, falling back to interactive prompts.
		urlValue := configInitUrl
		tokenValue := configInitToken
		projectValue := configInitProject
		nameValue := configInitName

		interactive := !cmd.Flags().Changed("token") || !cmd.Flags().Changed("project")

		if !cmd.Flags().Changed("url") && interactive {
			urlValue = prompt("API endpoint URL", configInitUrl)
		}
		if !cmd.Flags().Changed("token") {
			tokenValue = promptSecret("API token")
		}
		if tokenValue == "" {
			return fmt.Errorf("token is required")
		}

		// Validate credentials.
		fmt.Fprint(os.Stderr, "Verifying credentials... ")
		if _, err := api.NewClient(urlValue, tokenValue); err != nil {
			fmt.Fprintln(os.Stderr, "failed.")
			return fmt.Errorf("verify credentials: %w", err)
		}
		fmt.Fprintln(os.Stderr, "done.")

		if !cmd.Flags().Changed("project") {
			projectValue = prompt("Project", "")
		}
		if projectValue == "" {
			return fmt.Errorf("project is required")
		}

		if !cmd.Flags().Changed("name") && interactive {
			nameValue = prompt("Context name", configInitName)
		}

		// Check for existing context with the same name.
		_, endpointExists := globalConfiguration.Endpoints[nameValue]
		_, contextExists := globalConfiguration.Contexts[nameValue]
		if endpointExists || contextExists {
			if !configInitForce {
				if interactive {
					answer := prompt(fmt.Sprintf("Context %q already exists. Overwrite?", nameValue), "n")
					answer = strings.ToLower(strings.TrimSpace(answer))
					if answer != "y" && answer != "yes" {
						return fmt.Errorf("aborted")
					}
				} else {
					return fmt.Errorf("context %q already exists (use --force to overwrite)", nameValue)
				}
			}
		}

		// Write config.
		if globalConfiguration.Endpoints == nil {
			globalConfiguration.Endpoints = map[string]config.APIEndpoint{}
		}
		if globalConfiguration.Contexts == nil {
			globalConfiguration.Contexts = map[string]config.CLIContext{}
		}
		globalConfiguration.Endpoints[nameValue] = config.APIEndpoint{
			Token: tokenValue,
			URL:   urlValue,
		}
		globalConfiguration.Contexts[nameValue] = config.CLIContext{
			Endpoint: nameValue,
			Project:  projectValue,
		}
		globalConfiguration.ActiveContext = nameValue
		if err := globalConfiguration.Write(); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Configuration written to %s.\n", globalConfiguration.Path)
		return nil
	},
}

func initConfigCmd() {
	configInitCmd.Flags().StringVar(&configInitUrl, "url", "https://api.valar.dev/v2", "API endpoint URL")
	configInitCmd.Flags().StringVar(&configInitToken, "token", "", "API token")
	configInitCmd.Flags().StringVar(&configInitProject, "project", "", "Project name")
	configInitCmd.Flags().StringVar(&configInitName, "name", "default", "Context name")
	configInitCmd.Flags().BoolVar(&configInitForce, "force", false, "Overwrite existing context")
	configEndpointSetCmd.Flags().StringVar(&configEndpointSetToken, "token", "", "Token to use")
	configEndpointSetCmd.Flags().StringVar(&configEndpointSetUrl, "url", "", "URL the API can be reached on")
	configEndpointCmd.AddCommand(configEndpointSetCmd)
	configEndpointCmd.AddCommand(configEndpointRemoveCmd)
	configContextSetCmd.Flags().StringVar(&configContextSetProject, "project", "", "Project to use")
	configContextSetCmd.Flags().StringVar(&configContextSetEndpoint, "endpoint", "", "API endpoint")
	configContextCmd.AddCommand(configContextUseCmd)
	configContextCmd.AddCommand(configContextSetCmd)
	configContextCmd.AddCommand(configContextRemoveCmd)
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configContextCmd)
	configCmd.AddCommand(configEndpointCmd)
	configCmd.AddCommand(configViewCmd)
	rootCmd.AddCommand(configCmd)
}
