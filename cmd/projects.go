package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"valar/cli/pkg/config"

	"github.com/spf13/cobra"
)

var (
	authCmd = &cobra.Command{
		Use:   "auth",
		Short: "Manage project permissions",
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			client, err := globalConfiguration.APIClient()
			if err != nil {
				return err
			}
			var project string
			cfg := &config.ServiceConfig{}
			if err := cfg.ReadFromFile(functionConfiguration); err != nil {
				// Use default project
				project = globalConfiguration.Project()
			} else {
				project = cfg.Project
			}
			pms, err := client.ListPermissions(project)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
			fmt.Fprintln(tw, "USER\tACTIONS")
			for user, actions := range pms {
				fmt.Fprintf(tw, "%s\t%s\n", user, strings.Join(actions, ", "))
			}
			tw.Flush()
			return nil
		}),
	}
	authUser, authAction string
	authAllowCmd         = &cobra.Command{
		Use:   "allow",
		Short: "Allow a user to perform a specific action",
		Args:  cobra.ExactArgs(0),
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			client, err := globalConfiguration.APIClient()
			if err != nil {
				return err
			}
			var project string
			cfg := &config.ServiceConfig{}
			if err := cfg.ReadFromFile(functionConfiguration); err != nil {
				project = globalConfiguration.Project()
			} else {
				project = cfg.Project
			}
			if err := client.ModifyPermission(project, authUser, authAction, false); err != nil {
				return err
			}
			return nil
		}),
	}
	authForbidCmd = &cobra.Command{
		Use:   "forbid",
		Short: "Forbid a user to perform a specific action",
		Args:  cobra.ExactArgs(0),
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			client, err := globalConfiguration.APIClient()
			if err != nil {
				return err
			}
			var project string
			cfg := &config.ServiceConfig{}
			if err := cfg.ReadFromFile(functionConfiguration); err != nil {
				project = globalConfiguration.Project()
			} else {
				project = cfg.Project
			}
			if err := client.ModifyPermission(project, authUser, authAction, true); err != nil {
				return err
			}
			return nil
		}),
	}
)

func initProjectsCmd() {
	authCmd.AddCommand(authAllowCmd, authForbidCmd)
	authForbidCmd.Flags().StringVarP(&authAction, "action", "a", "invoke", "Action to be modified")
	authForbidCmd.Flags().StringVarP(&authUser, "user", "u", "anonymous", "User to be modified")
	authAllowCmd.Flags().StringVarP(&authAction, "action", "a", "invoke", "Action to be modified")
	authAllowCmd.Flags().StringVarP(&authUser, "user", "u", "anonymous", "User to be modified")
	rootCmd.AddCommand(authCmd)
}
