package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"valar/cli/pkg/api"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var version string

var rootCmd = &cobra.Command{
	Use:   "valar",
	Short: "Valar is a next-generation serverless platform",
	Long: `Valar is a next-generation serverless platform.

You code. We do the rest.
We take care while you do what you do best.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		cfgpath, ok := os.LookupEnv("VALARCONFIG")
		if !ok {
			homedir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			dirpath := filepath.Join(homedir, ".valar")
			// Make sure dirpath exists
			if err := os.MkdirAll(dirpath, 0755); err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			// Make sure config exists
			cfgpath = filepath.Join(dirpath, "config")
		}
		// Load files from cfgpath and merge them
		cfgpaths := strings.Split(cfgpath, ";")
		cfg := valarConfig{
			Endpoints: make(map[string]valarEndpoint),
			Contexts:  make(map[string]valarContext),
		}
		for _, path := range cfgpaths {
			data, err := os.ReadFile(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Could not read config file %s: %s\n", path, err)
				continue
			}
			// Decode config
			var subcfg valarConfig
			if err := yaml.Unmarshal(data, &subcfg); err != nil {
				return fmt.Errorf("unmarshal config: %w", err)
			}
			// Override cfg
			if subcfg.ActiveContext != "" {
				cfg.ActiveContext = subcfg.ActiveContext
			}
			for name, ctx := range subcfg.Contexts {
				cfg.Contexts[name] = ctx
			}
			for name, ep := range subcfg.Endpoints {
				cfg.Endpoints[name] = ep
			}
		}
		// Extract project, token, endpoint
		cfg.Path = cfgpaths[len(cfgpaths)-1]
		globalConfiguration = cfg

		// Warn if either project, token or endpoint is empty
		if len(globalConfiguration.Token()) == 0 || len(globalConfiguration.Endpoint()) == 0 || len(globalConfiguration.Project()) == 0 {
			fmt.Fprintln(os.Stderr, "Warning: Active context is invalid. Choose or configure a new context using the `valar config` command.")
		}
		return nil
	},
}

var globalConfiguration valarConfig

type valarConfig struct {
	ActiveContext string                   `yaml:"activeContext"`
	Endpoints     map[string]valarEndpoint `yaml:"endpoints"`
	Contexts      map[string]valarContext  `yaml:"contexts"`

	Path string `yaml:"-"`
}

func (cfg *valarConfig) Token() string {
	return cfg.Endpoints[cfg.Contexts[cfg.ActiveContext].Endpoint].Token
}

func (cfg *valarConfig) Endpoint() string {
	return cfg.Endpoints[cfg.Contexts[cfg.ActiveContext].Endpoint].URL
}

func (cfg *valarConfig) Project() string {
	return cfg.Contexts[cfg.ActiveContext].Project
}

func (cfg *valarConfig) APIClient() (*api.Client, error) {
	return api.NewClient(cfg.Endpoint(), cfg.Token())
}

func (cfg *valarConfig) Write() error {
	f, err := os.Create(cfg.Path)
	if err != nil {
		return fmt.Errorf("create config: %w", err)
	}
	defer f.Close()
	encoder := yaml.NewEncoder(f)
	defer encoder.Close()
	encoder.SetIndent(2)
	return encoder.Encode(cfg)
}

type valarEndpoint struct {
	Token string `yaml:"token"`
	URL   string `yaml:"url"`
}

type valarContext struct {
	Endpoint string `yaml:"endpoint"`
	Project  string `yaml:"project"`
}

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
