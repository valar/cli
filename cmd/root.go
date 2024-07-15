package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/valar/cli/config"
)

var version string

var rootCmd = &cobra.Command{
	Use:   "valar",
	Short: "Valar is a next-generation serverless platform.",
	Long: `Valar is a next-generation serverless platform.

You code. We do the rest.
We take care while you do what you do best.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		globalConfiguration, err = config.NewCLIConfigFromEnvironment()
		if err != nil {
			return fmt.Errorf("configure interface: %w", err)
		}
		return nil
	},
}

var globalConfiguration *config.CLIConfig

func init() {
	// Try to load config, if not found we're fine
	rootCmd.SetVersionTemplate("Valar CLI {{.Version}}\n")
	rootCmd.Version = version
	// Configure projects.go
	initProjectsCmd()
	// Configure deployments.go
	initDeploymentsCmd()
	// Configure builds.go
	initBuildsCmd()
	// Configure services.go
	initServicesCmd()
	// Configure env.go
	initEnvCmd()
	// Configure domains.go
	initDomainsCmd()
	// Configure config.go
	initConfigCmd()
	// Configure cron.go
	initCronCmd()
}

func runAndHandle(f func(*cobra.Command, []string) error) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		if err := f(cmd, args); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
