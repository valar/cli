package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"valar/cli/pkg/api"
	"valar/cli/pkg/config"

	"github.com/mholt/archiver/v3"
	"github.com/spf13/cobra"
)

const functionConfiguration = ".valar.yml"

var initEnv, initProject string
var initForce bool

var initCmd = &cobra.Command{
	Use:   "init [service]",
	Short: "Create a service configuration file",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cfg := &config.Config{
			Project:  initProject,
			Function: args[0],
		}
		cfg.Environment.Name = initEnv
		if _, err := os.Stat(functionConfiguration); err == nil && !initForce {
			fmt.Println("Configuration already exists, please use --force flag to override")
			return
		}
		if err := cfg.WriteToFile(functionConfiguration); err != nil {
			fmt.Println(err)
		}
	},
}

var pushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push a new version to Valar",
	Args:  cobra.ExactArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		// Load configuration
		cfg := &config.Config{}
		if err := cfg.ReadFromFile(functionConfiguration); err != nil {
			fmt.Println("Missing configuration, please set up your folder using valar init")
			return
		}
		// TODO: Stream to http writer
		// Generate source pkg
		tmppath := filepath.Join(os.TempDir(), "valar."+cfg.Project+"."+cfg.Function+".tar.gz")
		targz := archiver.NewTarGz()
		if err := targz.Archive([]string{"."}, tmppath); err != nil {
			fmt.Println("The package compression failed, please try again")
			return
		}
		defer os.Remove(tmppath)
		targzFile, err := os.Open(tmppath)
		if err != nil {
			fmt.Println("The package compression failed, please try again")
			return
		}
		defer targzFile.Close()
		// Connect to server
		client := api.NewClient(endpoint, token)
		task, err := client.SubmitBuild(cfg.Project, cfg.Function, cfg.Environment.Name, targzFile)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println(task.ID)
	},
}

func init() {
	initPf := initCmd.PersistentFlags()
	initPf.StringVarP(&initEnv, "env", "e", "", "build constructor environment")
	initPf.StringVarP(&initProject, "project", "p", "", "project name")
	initPf.BoolVarP(&initForce, "force", "f", false, "allow configuration override")
	cobra.MarkFlagRequired(initPf, "env")
	cobra.MarkFlagRequired(initPf, "project")

	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(pushCmd)
}
