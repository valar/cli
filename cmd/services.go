package cmd

import (
	"fmt"
	"os"
	"valar/cli/pkg/api"
	"valar/cli/pkg/config"
	"valar/cli/pkg/util"

	"github.com/spf13/cobra"
)

const functionConfiguration = ".valar.yml"

var initIgnore []string
var initEnv, initProject string
var initForce bool

var initCmd = &cobra.Command{
	Use:   "init [service]",
	Short: "Create a service configuration file",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cfg := &config.Config{
			Ignore:   initIgnore,
			Project:  initProject,
			Function: args[0],
		}
		cfg.Environment.Name = initEnv
		if _, err := os.Stat(functionConfiguration); err == nil && !initForce {
			fmt.Fprintln(os.Stderr, "Configuration already exists, please use --force flag to override")
			os.Exit(1)
			return
		}
		if err := cfg.WriteToFile(functionConfiguration); err != nil {
			fmt.Fprintln(os.Stderr, "Configuration write failed:", err)
			os.Exit(1)
			return
		}
	},
}

func pushFolder(cfg *config.Config, folder string) (*api.Task, error) {
	archivePath, err := util.CompressDir(folder, cfg.Ignore)
	if err != nil {
		return nil, fmt.Errorf("package compression failed: %w", err)
	}
	defer os.Remove(archivePath)
	targzFile, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("package archive failed: %w", err)
	}
	defer targzFile.Close()
	client := api.NewClient(endpoint, token)
	task, err := client.SubmitBuild(cfg.Project, cfg.Function, cfg.Environment.Name, targzFile)
	if err != nil {
		return nil, fmt.Errorf("build submit: %w", err)
	}
	return task, nil
}

var pushCmd = &cobra.Command{
	Use:   "push [folder]",
	Short: "Push a new version to Valar",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		// Load configuration
		cfg := &config.Config{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			fmt.Fprintln(os.Stderr, "Missing configuration, please set up your folder using valar init")
			os.Exit(1)
		}
		folder, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Locating working directory:", err)
			os.Exit(1)
		}
		if len(args) != 0 {
			folder = args[0]
		}
		task, err := pushFolder(cfg, folder)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Pushing folder:", err)
			os.Exit(1)
		}
		fmt.Println(task.ID)
	},
}

func init() {
	initPf := initCmd.PersistentFlags()
	initPf.StringArrayVarP(&initIgnore, "ignore", "i", []string{".valar.yml", ".git", "node_modules"}, "ignore files")
	initPf.StringVarP(&initEnv, "type", "t", "", "build constructor type")
	initPf.StringVarP(&initProject, "project", "p", "", "project name")
	initPf.BoolVarP(&initForce, "force", "f", false, "allow configuration override")
	cobra.MarkFlagRequired(initPf, "type")
	cobra.MarkFlagRequired(initPf, "project")

	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(pushCmd)
}
