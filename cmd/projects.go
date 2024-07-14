package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/valar/cli/api"
	"github.com/valar/cli/config"
)

var (
	authListCmd = &cobra.Command{
		Use:   "list [path]",
		Short: "List permissions for a path prefix",
		Args:  cobra.MaximumNArgs(1),
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			client, err := globalConfiguration.APIClient()
			if err != nil {
				return err
			}
			cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, nil, globalConfiguration)
			if err != nil {
				return err
			}
			namespace, prefix := "service", cfg.Project()
			if len(args) == 1 {
				subargs := strings.SplitN(args[0], ":", 2)
				if len(subargs) != 2 {
					return fmt.Errorf("expect path to be in the form namespace:prefix")
				}
				namespace, prefix = subargs[0], subargs[1]
			}
			permissions, err := client.ListPermissions(cfg.Project(), namespace, prefix)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
			fmt.Fprintln(tw, "PATH\tUSER\tACTION\tSTATE")
			for _, pm := range permissions {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", pm.Path, pm.User, pm.Action, pm.State)
			}
			tw.Flush()
			return nil
		}),
	}

	authAllowCmd = &cobra.Command{
		Use:   "allow path user action",
		Short: "Modify permissions for a path and user",
		Args:  cobra.ExactArgs(3),
		Run:   runAndHandle(authModifyWithState("allow")),
	}
	authForbidCmd = &cobra.Command{
		Use:   "forbid path user action",
		Short: "Forbid a specific action for a path and user",
		Args:  cobra.ExactArgs(3),
		Run:   runAndHandle(authModifyWithState("forbid")),
	}
	authClearCmd = &cobra.Command{
		Use:   "clear path user action",
		Short: "Remove the permission for a path and user",
		Args:  cobra.ExactArgs(3),
		Run:   runAndHandle(authModifyWithState("unset")),
	}
	authCheckCmd = &cobra.Command{
		Use:   "check path user action",
		Short: "Check if a user can perform an action",
		Args:  cobra.ExactArgs(3),
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			client, err := globalConfiguration.APIClient()
			if err != nil {
				return err
			}
			cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, nil, globalConfiguration)
			if err != nil {
				return err
			}
			path, err := api.PermissionPathFromString(args[0])
			if err != nil {
				return err
			}
			user, err := api.PermissionUserFromString(args[1])
			if err != nil {
				return err
			}
			permission := api.Permission{
				Path:   path,
				User:   user,
				Action: args[2],
			}
			allowed, err := client.CheckPermission(cfg.Project(), permission)
			if err != nil {
				return err
			}
			if allowed {
				fmt.Println("allowed")
			} else {
				fmt.Println("forbidden")
			}
			return nil
		}),
	}

	authCmd = &cobra.Command{
		Use:   "auth",
		Short: "Manage user and service permissions",
	}
)

func authModifyWithState(state string) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, nil, globalConfiguration)
		if err != nil {
			return err
		}
		path, err := api.PermissionPathFromString(args[0])
		if err != nil {
			return err
		}
		user, err := api.PermissionUserFromString(args[1])
		if err != nil {
			return err
		}
		permission := api.Permission{
			Path:   path,
			User:   user,
			Action: args[2],
			State:  state,
		}
		modified, err := client.ModifyPermission(cfg.Project(), permission)
		if err != nil {
			return err
		}
		if modified {
			fmt.Println("modified")
		} else {
			fmt.Println("unchanged")
		}
		return nil
	}
}

func initProjectsCmd() {
	authCmd.AddCommand(authListCmd, authAllowCmd, authForbidCmd, authClearCmd, authCheckCmd)
	rootCmd.AddCommand(authCmd)
}
