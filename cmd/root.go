package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var token, endpoint string
var version = "v0.0.0"

var rootCmd = &cobra.Command{
	Use:   "valar",
	Short: "Valar is a next-generation serverless provider",
	Long: `Valar is a next-generation serverless provider.

You code. We do the rest.
We take care
while you do
what you do best.`,
	Version: version,
}

func init() {
	viper.SetConfigName("valar.cloud")
	viper.AddConfigPath("/etc/valar/")
	viper.AddConfigPath("$HOME/.valar")
	viper.AddConfigPath(".")
	viper.ReadInConfig()
	viper.SetEnvPrefix("valar")
	viper.AutomaticEnv()
	rootCmd.PersistentFlags().StringVar(&token, "api-token", viper.GetString("token"), "Valar API token")
	rootCmd.PersistentFlags().StringVar(&endpoint, "api-endpoint", viper.GetString("endpoint"), "Valar API endpoint")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
