package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/valar/cli/api"
	"gopkg.in/yaml.v3"
)

func NewCLIConfigFromEnvironment() (*CLIConfig, error) {
	cfgpath, ok := os.LookupEnv("VALARCONFIG")
	if !ok {
		homedir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("load config: %w", err)
		}
		dirpath := filepath.Join(homedir, ".valar")
		// Make sure dirpath exists
		if err := os.MkdirAll(dirpath, 0755); err != nil {
			return nil, fmt.Errorf("load config: %w", err)
		}
		// Make sure config exists
		cfgpath = filepath.Join(dirpath, "config")
	}
	// Load files from cfgpath and merge them
	cfgpaths := strings.Split(cfgpath, ";")
	cfg := &CLIConfig{
		Endpoints: map[string]APIEndpoint{},
		Contexts:  map[string]CLIContext{},
	}
	for _, path := range cfgpaths {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Could not read config file %s: %s\n", path, err)
			continue
		}
		// Decode config
		var subcfg CLIConfig
		if err := yaml.Unmarshal(data, &subcfg); err != nil {
			return nil, fmt.Errorf("unmarshal config: %w", err)
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
	return cfg, nil
}

type CLIConfig struct {
	Version       string                 `yaml:"version"`
	ActiveContext string                 `yaml:"activeContext"`
	Endpoints     map[string]APIEndpoint `yaml:"endpoints"`
	Contexts      map[string]CLIContext  `yaml:"contexts"`

	Path string `yaml:"-"`
}

func (cfg *CLIConfig) Token() string {
	token := cfg.Endpoints[cfg.Contexts[cfg.ActiveContext].Endpoint].Token
	if len(token) == 0 {
		fmt.Fprintln(os.Stderr, "Operation requires valid endpoint token.")
		os.Exit(1)
	}
	return token
}

func (cfg *CLIConfig) Endpoint() string {
	url := cfg.Endpoints[cfg.Contexts[cfg.ActiveContext].Endpoint].URL
	if len(url) == 0 {
		fmt.Fprintln(os.Stderr, "Operation requires valid endpoint URL.")
		os.Exit(1)
	}
	return url
}

func (cfg *CLIConfig) Project() string {
	project := cfg.Contexts[cfg.ActiveContext].Project
	if len(project) == 0 {
		fmt.Fprintln(os.Stderr, "Operation requires valid context project.")
		os.Exit(1)
	}
	return project
}

func (cfg *CLIConfig) APIClient() (*api.Client, error) {
	return api.NewClient(cfg.Endpoint(), cfg.Token())
}

func (cfg *CLIConfig) Write() error {
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

type APIEndpoint struct {
	Token string `yaml:"token"`
	URL   string `yaml:"url"`
}

type CLIContext struct {
	Endpoint string `yaml:"endpoint"`
	Project  string `yaml:"project"`
}
