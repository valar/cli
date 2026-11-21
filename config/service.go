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
	Key    string `json:"key"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
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
	Constructor string              `yaml:"constructor,omitempty" json:"constructor" doc:"Constructor that builds the service, such as go, node, python, static or docker."`
	Ignore      []string            `yaml:"ignore" json:"ignore" doc:"Path prefixes, relative to the pushed folder, left out of the upload."`
	Environment []EnvironmentConfig `yaml:"environment" json:"environment" doc:"Deprecated: build variables are stored by the platform, see 'valar env'. Move them with 'valar env import'."`
}

type DeploymentConfig struct {
	Skip        bool                `yaml:"skip" json:"skip" doc:"Build without deploying."`
	Environment []EnvironmentConfig `yaml:"environment" json:"environment" doc:"Deprecated: runtime variables are stored by the platform, see 'valar env'. Move them with 'valar env import'."`
	CPU         *int                `yaml:"cpu,omitempty" json:"cpu,omitempty" doc:"Requested CPU in millicores, 100 to 2000 in steps of 100; the platform's default is 1000. Applied before each deploying push."`
	Memory      *int                `yaml:"memory,omitempty" json:"memory,omitempty" doc:"Requested memory in MiB, 128 to 4096; the platform's default is 1024. Applied before each deploying push."`
	HTTP        *bool               `yaml:"http,omitempty" json:"http,omitempty" doc:"Whether the service serves HTTP; the platform's default is true. Applied before each deploying push."`
	MinRunning  *int                `yaml:"minRunning,omitempty" json:"minRunning,omitempty" doc:"Instances that are never paused; the platform's default is 0. Applied before each deploying push."`
}

// Settings returns the service settings the file declares, or nil.
func (d *DeploymentConfig) Settings() map[string]any {
	if d == nil {
		return nil
	}
	settings := map[string]any{}
	if d.CPU != nil {
		settings["cpu"] = *d.CPU
	}
	if d.Memory != nil {
		settings["memory"] = *d.Memory
	}
	if d.HTTP != nil {
		settings["http"] = *d.HTTP
	}
	if d.MinRunning != nil {
		settings["minRunning"] = *d.MinRunning
	}
	if len(settings) == 0 {
		return nil
	}
	return settings
}

// Abort reports a missing setting and ends the process. The CLI replaces it to
// apply its exit codes and output format.
var Abort = func(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
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
		Abort("Operation requires service project.")
	}
	return w.yaml.Project
}

func (w *ValidatedServiceConfig) Service() string {
	if w.yaml.Service == "" {
		Abort("Operation requires service reference (may be specified using --service).")
	}
	return w.yaml.Service
}

func (w *ValidatedServiceConfig) Build() BuildConfig {
	if w.yaml.Build == nil {
		Abort("Operation requires build specification.")
	}
	return *w.yaml.Build
}

func (w *ValidatedServiceConfig) Deployment() DeploymentConfig {
	if w.yaml.Deployment == nil {
		Abort("Operation requires deployment specification.")
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

// NewServiceConfigWithFallback reads the service configuration at path, or
// falls back to the CLI's project. VALAR_PROJECT overrides the project and a
// non-empty service the service; the build and deployment sections stay.
func NewServiceConfigWithFallback(path string, service *string, cli *CLIConfig) (*ValidatedServiceConfig, error) {
	cfg := ServiceConfigYAML{}
	if err := cfg.ReadFromFile(path); errors.Is(err, os.ErrNotExist) {
		cfg = ServiceConfigYAML{Project: cli.Project()}
	} else if err != nil {
		return nil, err
	}
	if cli.env.Project != "" {
		cfg.Project = cli.env.Project
	}
	if service != nil && *service != "" {
		cfg.Service = *service
	}
	return &ValidatedServiceConfig{yaml: cfg}, nil
}

type ServiceConfigYAML struct {
	Project    string            `yaml:"project,omitempty" json:"project" doc:"Project the service belongs to."`
	Service    string            `yaml:"service,omitempty" json:"service" doc:"Name of the service."`
	Build      *BuildConfig      `yaml:"build" json:"build" doc:"How the service is built."`
	Deployment *DeploymentConfig `yaml:"deployment" json:"deployment" doc:"How builds are deployed."`

	filePath string
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
		var doc yaml.Node
		if err := yaml.NewDecoder(fd).Decode(&doc); err != nil {
			return &Error{Path: path, Err: fmt.Errorf("config read: %w", err)}
		}
		if err := validate(&doc, Schema()); err != nil {
			return &Error{Path: path, Err: fmt.Errorf("%s: %w", path, err)}
		}
		if err := doc.Decode(config); err != nil {
			return &Error{Path: path, Err: fmt.Errorf("%s: %w", path, err)}
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
