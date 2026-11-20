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
We take care while you do what you do best.

With --json, or VALAR_OUTPUT=json, a command prints its result as one JSON
document on stdout, logs as one JSON object per line, and an error as
{"error": {"kind": "...", "message": "..."}} on stderr. Progress goes to
stderr.

` + exitCodeHelp,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		globalConfiguration, err = config.NewCLIConfigFromEnvironment()
		if err != nil {
			return withKind(kindInvalid, fmt.Errorf("configure interface: %w", err))
		}
		return nil
	},
}

var globalConfiguration *config.CLIConfig

func init() {
	// Try to load config, if not found we're fine
	rootCmd.SetVersionTemplate("Valar CLI {{.Version}}\n")
	rootCmd.Version = version
	rootCmd.PersistentFlags().BoolVar(&outputJSON, "json", false, "Print results as JSON and errors as JSON on stderr (or set VALAR_OUTPUT=json)")
	config.Abort = func(msg string) {
		exitWithError(newError(kindInvalid, "%s", msg), kindInvalid, false)
	}
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
	// Configure batch.go
	initBatchCmd()
}

// runAndHandle prints the error of f and exits with the code of its kind.
func runAndHandle(f func(*cobra.Command, []string) error) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		if err := f(cmd, args); err != nil {
			exitWithError(err, kindFailed, false)
		}
	}
}

// Execute runs the CLI. Errors that reach it come from cobra's argument and
// flag checks, loading the CLI configuration, or a RunE command; cobra has
// printed them unless JSON output is on.
func Execute() {
	outputJSON = jsonRequested(os.Args[1:], os.Getenv("VALAR_OUTPUT"))
	if outputJSON {
		rootCmd.SilenceErrors = true
		rootCmd.SilenceUsage = true
	}
	if err := rootCmd.Execute(); err != nil {
		exitWithError(err, kindInvalid, !outputJSON)
	}
}
