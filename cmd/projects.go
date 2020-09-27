package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"valar/cli/pkg/api"
	"valar/cli/pkg/config"

	"github.com/go-yaml/yaml"
	"github.com/spf13/cobra"
)

var (
	configCmd = &cobra.Command{
		Use:   "config",
		Short: "Write current configuration to disk",
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			homedir, _ := os.UserHomeDir()
			if err := os.MkdirAll(filepath.Join(homedir, ".valar"), 0755); err != nil {
				return err
			}
			cfgfile, err := os.Create(filepath.Join(homedir, ".valar/valarcfg"))
			if err != nil {
				return err
			}
			defer cfgfile.Close()
			bytes, err := yaml.Marshal(struct {
				APIToken    string `yaml:"token"`
				APIEndpoint string `yaml:"endpoint"`
			}{
				token, endpoint,
			})
			if err != nil {
				return err
			}
			if _, err := cfgfile.Write(bytes); err != nil {
				return err
			}
			return nil
		}),
	}
	authCmd = &cobra.Command{
		Use:   "auth",
		Short: "Manage project permissions",
		Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
			cfg := &config.ServiceConfig{}
			if err := cfg.ReadFromFile(functionConfiguration); err != nil {
				return err
			}
			client, err := api.NewClient(endpoint, token)
			if err != nil {
				return err
			}
			pms, err := client.ListPermissions(cfg.Project)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
			fmt.Fprintln(tw, "User\tActions")
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
			cfg := &config.ServiceConfig{}
			if err := cfg.ReadFromFile(functionConfiguration); err != nil {
				return err
			}
			client, err := api.NewClient(endpoint, token)
			if err != nil {
				return err
			}
			if err := client.ModifyPermission(cfg.Project, authUser, authAction, false); err != nil {
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
			cfg := &config.ServiceConfig{}
			if err := cfg.ReadFromFile(functionConfiguration); err != nil {
				return err
			}
			client, err := api.NewClient(endpoint, token)
			if err != nil {
				return err
			}
			if err := client.ModifyPermission(cfg.Project, authUser, authAction, true); err != nil {
				return err
			}
			return nil
		}),
	}
)

func init() {
	authCmd.AddCommand(authAllowCmd, authForbidCmd)
	authForbidCmd.Flags().StringVarP(&authAction, "action", "a", "invoke", "Action to be modified")
	authForbidCmd.Flags().StringVarP(&authUser, "user", "u", "anonymous", "User to be modified")
	authAllowCmd.Flags().StringVarP(&authAction, "action", "a", "invoke", "Action to be modified")
	authAllowCmd.Flags().StringVarP(&authUser, "user", "u", "anonymous", "User to be modified")
	rootCmd.AddCommand(authCmd)
	rootCmd.AddCommand(configCmd)
}
