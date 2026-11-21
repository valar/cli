package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/valar/cli/api"
	"gopkg.in/yaml.v3"
)

// DefaultEndpoint is the API used when VALAR_TOKEN is set and neither the
// configuration nor VALAR_ENDPOINT names an endpoint.
const DefaultEndpoint = "https://api.valar.dev/v2"

// Environment variables that take precedence over the active context, so CI
// needs no configuration file.
const (
	EnvToken    = "VALAR_TOKEN"
	EnvEndpoint = "VALAR_ENDPOINT"
	EnvProject  = "VALAR_PROJECT"
)

func NewCLIConfigFromEnvironment() (*CLIConfig, error) {
	cfgpath, explicit := os.LookupEnv("VALARCONFIG")
	if !explicit {
		homedir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("load config: %w", err)
		}
		dirpath := filepath.Join(homedir, ".valar")
		// Make sure dirpath exists
		if err := os.MkdirAll(dirpath, 0o700); err != nil {
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
		if !explicit && errors.Is(err, os.ErrNotExist) {
			continue
		}
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
	cfg.Path = cfgpaths[len(cfgpaths)-1]
	cfg.env = envOverrides{
		Token:    os.Getenv(EnvToken),
		Endpoint: os.Getenv(EnvEndpoint),
		Project:  os.Getenv(EnvProject),
	}
	return cfg, nil
}

// envOverrides holds the VALAR_* variables; they are never written back.
type envOverrides struct {
	Token, Endpoint, Project string
}

type CLIConfig struct {
	Version       string                 `yaml:"version" json:"version"`
	ActiveContext string                 `yaml:"activeContext" json:"activeContext"`
	Endpoints     map[string]APIEndpoint `yaml:"endpoints" json:"endpoints"`
	Contexts      map[string]CLIContext  `yaml:"contexts" json:"contexts"`

	Path string `yaml:"-" json:"-"`

	env envOverrides
}

func (cfg *CLIConfig) Token() string {
	if cfg.env.Token != "" {
		return cfg.env.Token
	}
	token := cfg.Endpoints[cfg.Contexts[cfg.ActiveContext].Endpoint].Token
	if len(token) == 0 {
		Abort("Operation requires valid endpoint token.")
	}
	return token
}

func (cfg *CLIConfig) Endpoint() string {
	if cfg.env.Endpoint != "" {
		return cfg.env.Endpoint
	}
	url := cfg.Endpoints[cfg.Contexts[cfg.ActiveContext].Endpoint].URL
	if len(url) == 0 && cfg.env.Token != "" {
		return DefaultEndpoint
	}
	if len(url) == 0 {
		Abort("Operation requires valid endpoint URL.")
	}
	return url
}

func (cfg *CLIConfig) Project() string {
	if cfg.env.Project != "" {
		return cfg.env.Project
	}
	project := cfg.Contexts[cfg.ActiveContext].Project
	if len(project) == 0 {
		Abort("Operation requires valid context project.")
	}
	return project
}

func (cfg *CLIConfig) APIClient() (*api.Client, error) {
	return api.NewClient(cfg.Endpoint(), cfg.Token())
}

// Write saves the configuration, which holds tokens, readable only by its
// owner. A file other users could read is tightened first, with a note on stderr.
func (cfg *CLIConfig) Write() error {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	info, statErr := os.Stat(cfg.Path)
	f, err := os.OpenFile(cfg.Path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create config: %w", err)
	}
	defer f.Close()
	if statErr == nil && info.Mode().Perm()&0o077 != 0 {
		if err := f.Chmod(0o600); err != nil {
			return fmt.Errorf("restrict config: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Note: %s was open to other users; it is now readable only by you (mode 0600).\n", cfg.Path)
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return f.Close()
}

type APIEndpoint struct {
	Token string `yaml:"token" json:"token"`
	URL   string `yaml:"url" json:"url"`
}

type CLIContext struct {
	Endpoint string `yaml:"endpoint" json:"endpoint"`
	Project  string `yaml:"project" json:"project"`
}
