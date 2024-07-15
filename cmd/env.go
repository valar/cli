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

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage environment variables.",
}

var envListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all environment variables bound to the specified scope.",
	Args:  cobra.NoArgs,
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, nil, globalConfiguration)
		if err != nil {
			return err
		}
		kvs := cfg.Deployment().Environment
		if envBuild {
			kvs = cfg.Build().Environment
		}
		switch envFormat {
		case "raw":
			for _, kv := range kvs {
				fmt.Printf("%s=%s\n", kv.Key, kv.Value)
			}
		case "table":
			tw := tabwriter.NewWriter(os.Stdout, 0, 1, 1, ' ', 0)
			fmt.Fprintln(tw, "KEY\tVALUE\tSECRET")
			for _, kv := range kvs {
				fmt.Fprintf(tw, "%s\t%s\t%v\n", kv.Key, kv.Value, kv.Secret)
			}
			tw.Flush()
		default:
			return fmt.Errorf("unknown env format %s", envFormat)
		}
		return nil
	}),
}

var envFormat string
var envBuild bool
var envSetSecret bool

var envSetCmd = &cobra.Command{
	Use:   "set [envvar]=[value]",
	Short: "Set variable to the given value",
	Args:  cobra.ExactArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, nil, globalConfiguration)
		if err != nil {
			return err
		}
		client, err := globalConfiguration.APIClient()
		if err != nil {
			return err
		}
		// parse arg
		kvp := strings.SplitN(args[0], "=", 2)
		if len(kvp) != 2 {
			return fmt.Errorf("bad arg format, must be key=value")
		}
		kv := &api.KVPair{
			Key:   kvp[0],
			Value: kvp[1],
		}
		if envSetSecret {
			kv, err = client.EncryptEnvironment(cfg.Project(), cfg.Service(), &api.KVPair{
				Key:    kvp[0],
				Value:  kvp[1],
				Secret: true,
			})
			if err != nil {
				return err
			}
		}
		yamlCfg := cfg.Unwrap()
		target := &yamlCfg.Deployment.Environment
		if envBuild {
			target = &yamlCfg.Build.Environment
		}
		// Check for conflict
		replaced := false
		for i := range *target {
			if (*target)[i].Key == kv.Key {
				(*target)[i] = config.EnvironmentConfig(*kv)
				replaced = true
				break
			}
		}
		if !replaced {
			*target = append(*target, config.EnvironmentConfig(*kv))
		}
		return yamlCfg.WriteBack()
	}),
}

var envDeleteCmd = &cobra.Command{
	Use:   "delete [variable]",
	Short: "Delete the environment variable.",
	Args:  cobra.ExactArgs(1),
	Run: runAndHandle(func(cmd *cobra.Command, args []string) error {
		cfg, err := config.NewServiceConfigWithFallback(functionConfiguration, nil, globalConfiguration)
		if err != nil {
			return err
		}
		yamlCfg := cfg.Unwrap()
		target := &yamlCfg.Deployment.Environment
		if envBuild {
			target = &yamlCfg.Build.Environment
		}
		// If found, swap key to end and delete item
		index := -1
		for i := range *target {
			if (*target)[i].Key == args[0] {
				index = i
				break
			}
		}
		// If not found
		if index < 0 {
			return fmt.Errorf("key not found")
		}
		(*target)[index] = (*target)[len(*target)-1]
		(*target) = (*target)[:len(*target)-1]
		return yamlCfg.WriteBack()
	}),
}

func initEnvCmd() {
	envSetCmd.Flags().BoolVar(&envSetSecret, "secret", false, "Hide variable content in logs and other listings")
	envCmd.PersistentFlags().BoolVarP(&envBuild, "build", "b", false, "Build scope instead of deployments")
	envCmd.Flags().StringVar(&envFormat, "format", "table", "Choose display format (table|raw)")
	envCmd.AddCommand(envListCmd, envSetCmd, envDeleteCmd)
	rootCmd.AddCommand(envCmd)
}
