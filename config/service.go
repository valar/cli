package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type EnvironmentConfig struct {
	Key, Value string
	Secret     bool
}

func (e *EnvironmentConfig) MarshalYAML() (interface{}, error) {
	if e.Secret {
		return struct {
			Key    string `yaml:"key,omitempty"`
			Value  string `yaml:"value,omitempty"`
			Secret bool   `yaml:"secret,omitempty"`
		}{
			Key:    e.Key,
			Value:  e.Value,
			Secret: e.Secret,
		}, nil
	}
	return e.Key + "=" + e.Value, nil
}

func (e *EnvironmentConfig) UnmarshalYAML(value *yaml.Node) error {
	// Try to parse as raw string first
	var raw string
	if err := value.Decode(&raw); err == nil {
		envpair := strings.SplitN(raw, "=", 2)
		if len(envpair) != 2 {
			return fmt.Errorf("envvar in raw form has to be KEY=VALUE")
		}
		e.Key = envpair[0]
		e.Value = envpair[1]
		return nil
	}
	// Try to parse as struct
	var structured struct {
		Key    string `yaml:"key,omitempty"`
		Value  string `yaml:"value,omitempty"`
		Secret bool   `yaml:"secret,omitempty"`
	}
	if err := value.Decode(&structured); err != nil {
		return fmt.Errorf("envvar has to be in raw or structured form")
	}
	e.Key = structured.Key
	e.Value = structured.Value
	e.Secret = structured.Secret
	return nil
}

type BuildConfig struct {
	Constructor string              `yaml:"constructor,omitempty"`
	Ignore      []string            `yaml:"ignore"`
	Environment []EnvironmentConfig `yaml:"environment"`
}

type DeploymentConfig struct {
	Skip        bool                `yaml:"skip"`
	Environment []EnvironmentConfig `yaml:"environment"`
}

type ServiceConfig interface {
	Project() string
	Service() string
	Build() BuildConfig
	Deployment() DeploymentConfig
}

type ValidatedServiceConfig struct {
	yaml ServiceConfigYAML
}

func (w *ValidatedServiceConfig) Project() string {
	if w.yaml.Project == "" {
		fmt.Fprintln(os.Stderr, "Operation requires service project.")
		os.Exit(1)
	}
	return w.yaml.Project
}

func (w *ValidatedServiceConfig) Service() string {
	if w.yaml.Service == "" {
		fmt.Fprintln(os.Stderr, "Operation requires service reference (may be specified using --service).")
		os.Exit(1)
	}
	return w.yaml.Service
}

func (w *ValidatedServiceConfig) Build() BuildConfig {
	if w.yaml.Build == nil {
		fmt.Fprintln(os.Stderr, "Operation requires build specification.")
		os.Exit(1)
	}
	return *w.yaml.Build
}

func (w *ValidatedServiceConfig) Deployment() DeploymentConfig {
	if w.yaml.Deployment == nil {
		fmt.Fprintln(os.Stderr, "Operation requires deployment specification.")
		os.Exit(1)
	}
	return *w.yaml.Deployment
}

func (w *ValidatedServiceConfig) Unwrap() ServiceConfigYAML {
	return w.yaml
}

func NewServiceConfigFromFile(path string) (*ValidatedServiceConfig, error) {
	cfg := ServiceConfigYAML{}
	if err := cfg.ReadFromFile(path); err != nil {
		return nil, err
	}
	return &ValidatedServiceConfig{yaml: cfg}, nil
}

func NewServiceConfigWithFallback(path string, service *string, cli *CLIConfig) (*ValidatedServiceConfig, error) {
	cfg := ServiceConfigYAML{}
	if err := cfg.ReadFromFile(path); errors.Is(err, os.ErrNotExist) {
		if service == nil {
			return &ValidatedServiceConfig{ServiceConfigYAML{Project: cli.Project()}}, nil
		}
		return &ValidatedServiceConfig{ServiceConfigYAML{Project: cli.Project(), Service: *service}}, nil
	} else if err == nil && service != nil && len(*service) > 0 {
		return &ValidatedServiceConfig{ServiceConfigYAML{Project: cfg.Project, Service: *service}}, nil
	}
	return &ValidatedServiceConfig{yaml: cfg}, nil
}

type ServiceConfigYAML struct {
	Project    string            `yaml:"project,omitempty"`
	Service    string            `yaml:"service,omitempty"`
	Build      *BuildConfig      `yaml:"build"`
	Deployment *DeploymentConfig `yaml:"deployment"`

	filePath string `yaml:"-"`
}

func (config *ServiceConfigYAML) ReadFromFile(name string) error {
	// Do recursive discovery
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	parent := ""
	for cwd != parent {
		parent = cwd
		path := filepath.Join(filepath.Join(cwd, name))
		fd, err := os.Open(path)
		if err != nil {
			cwd, _ = filepath.Split(filepath.Clean(cwd))
			continue
		}
		defer fd.Close()
		decoder := yaml.NewDecoder(fd)
		if err := decoder.Decode(config); err != nil {
			return fmt.Errorf("config read: %w", err)
		}
		config.filePath = path
		return nil
	}
	return os.ErrNotExist
}

func (config *ServiceConfigYAML) WriteToFile(path string) error {
	fd, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("config write: %w", err)
	}
	defer fd.Close()
	encoder := yaml.NewEncoder(fd)
	if err := encoder.Encode(config); err != nil {
		return fmt.Errorf("config write: %w", err)
	}
	return nil
}

func (config *ServiceConfigYAML) WriteBack() error {
	return config.WriteToFile(config.filePath)
}
