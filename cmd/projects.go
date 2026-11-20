package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/juju/ansiterm"
	"github.com/spf13/cobra"
	"github.com/valar/cli/api"
	"github.com/valar/cli/config"
)

var (
	projectsCreatePublic bool
	projectsDeleteYes    bool
)

var (
	projectsCmd = &cobra.Command{
		Use:     "project",
		Short:   "Create and delete projects.",
		Aliases: []string{"projects"},
	}

	projectsCreateCmd = &cobra.Command{
		Use:   "create [--public] name",
		Short: "Create a project.",
		Long: `Create a project owned by you.

A name is 3 to 20 lowercase letters or digits and starts with a letter. A user
can create at most 3 projects. Anyone can invoke the services of a public
project without a token.`,
		Args: cobra.ExactArgs(1),
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			client, err := globalConfiguration.APIClient()
			if err != nil {
				return err
			}
			return createProject(client, args[0], projectsCreatePublic, os.Stdout)
		}),
	}

	projectsDeleteCmd = &cobra.Command{
		Use:   "delete [--yes] name",
		Short: "Delete an empty project and retire its name.",
		Long: `Delete an empty project and retire its name.

Only a project without services, domains and key-value storage can be deleted.
Its name is retired for good: no project can use it again. The command asks
for the project name to confirm, unless --yes is given.`,
		Args: cobra.ExactArgs(1),
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			client, err := globalConfiguration.APIClient()
			if err != nil {
				return err
			}
			var confirm io.Reader = os.Stdin
			if projectsDeleteYes {
				confirm = nil
			}
			return deleteProject(client, args[0], confirm, os.Stderr)
		}),
	}
)

func createProject(client *api.Client, name string, public bool, w io.Writer) error {
	project, err := client.CreateProject(name, public)
	if err != nil {
		return err
	}
	tw := ansiterm.NewTabWriter(w, 6, 0, 1, ' ', 0)
	fmt.Fprintf(tw, "Project:\t%s\n", project.Name)
	fmt.Fprintf(tw, "Creator:\t%s\n", project.Creator)
	if project.Public {
		fmt.Fprintf(tw, "Access:\tpublic, anyone can invoke its services\n")
	} else {
		fmt.Fprintf(tw, "Access:\tprivate\n")
	}
	return tw.Flush()
}

// deleteProject reads the project name from confirm before deleting, unless
// confirm is nil.
func deleteProject(client *api.Client, name string, confirm io.Reader, w io.Writer) error {
	if err := api.VerifyProjectName(name); err != nil {
		return err
	}
	if confirm != nil {
		fmt.Fprintf(w, "Deleting project %s retires its name for good: no project can use it again.\n", name)
		fmt.Fprint(w, "Type the project name to confirm: ")
		line, _ := bufio.NewReader(confirm).ReadString('\n')
		if strings.TrimSpace(line) != name {
			return fmt.Errorf("aborted: the input does not match %s, pass --yes to skip the confirmation", name)
		}
	}
	if err := client.DeleteProject(name); err != nil {
		return err
	}
	fmt.Fprintf(w, "Deleted project %s. Its name can't be used again.\n", name)
	return nil
}

var (
	authListCmd = &cobra.Command{
		Use:   "list [path]",
		Short: "List permissions for a path prefix.",
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
		Short: "Modify permissions for a path and user.",
		Args:  cobra.ExactArgs(3),
		Run:   runAndHandle(authModifyWithState("allow")),
	}
	authForbidCmd = &cobra.Command{
		Use:   "forbid path user action",
		Short: "Forbid a specific action for a path and user.",
		Args:  cobra.ExactArgs(3),
		Run:   runAndHandle(authModifyWithState("forbid")),
	}
	authClearCmd = &cobra.Command{
		Use:   "clear path user action",
		Short: "Remove the permission for a path and user.",
		Args:  cobra.ExactArgs(3),
		Run:   runAndHandle(authModifyWithState("unset")),
	}
	authCheckCmd = &cobra.Command{
		Use:   "check path user action",
		Short: "Check if a user can perform an action.",
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
		Short: "Manage user and service permissions.",
	}

	whoamiCmd = &cobra.Command{
		Use:   "whoami",
		Short: "Show the currently authenticated user.",
		Args:  cobra.NoArgs,
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			client, err := globalConfiguration.APIClient()
			if err != nil {
				return err
			}
			info, err := client.UserInfo()
			if err != nil {
				return err
			}
			tw := ansiterm.NewTabWriter(os.Stdout, 6, 0, 1, ' ', 0)
			fmt.Fprintf(tw, "User:\t%s\n", info.Name)
			fmt.Fprintf(tw, "Projects:\t%s\n", strings.Join(info.Projects, ", "))
			tw.Flush()
			return nil
		}),
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
	projectsCreateCmd.Flags().BoolVar(&projectsCreatePublic, "public", false, "Let anyone invoke the project's services without a token")
	projectsDeleteCmd.Flags().BoolVarP(&projectsDeleteYes, "yes", "y", false, "Delete without asking for the project name")
	projectsCmd.AddCommand(projectsCreateCmd, projectsDeleteCmd)
	rootCmd.AddCommand(projectsCmd)
	authCmd.AddCommand(authListCmd, authAllowCmd, authForbidCmd, authClearCmd, authCheckCmd)
	rootCmd.AddCommand(authCmd)
	rootCmd.AddCommand(whoamiCmd)
}
