package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var version string

var rootCmd = &cobra.Command{
	Use:   "valar",
	Short: "Valar is a next-generation serverless provider",
	Long: `Valar is a next-generation serverless provider.

You code. We do the rest.
We take care while you do what you do best.`,
}

var token, endpoint string

func init() {
	viper.SetConfigName("valar.cloud")
	viper.AddConfigPath("$HOME/.valar/")
	viper.AddConfigPath(".")
	viper.ReadInConfig()
	viper.SetEnvPrefix("VALAR")
	viper.AutomaticEnv()
	rootCmd.PersistentFlags().StringVar(&token, "api-token", viper.GetString("token"), "API token to use")
	rootCmd.PersistentFlags().StringVar(&endpoint, "api-endpoint", viper.GetString("endpoint"), "API endpoint to use")
	rootCmd.SetVersionTemplate("Valar CLI {{.Version}}\n")
	rootCmd.Version = version
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
