package config

import (
	"errors"
	"fmt"
	"os"
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

type ServiceConfig struct {
	Project    string           `yaml:"project,omitempty"`
	Service    string           `yaml:"service,omitempty"`
	Build      BuildConfig      `yaml:"build,omitempty"`
	Deployment DeploymentConfig `yaml:"deployment"`
}

func (config *ServiceConfig) ReadFromFile(path string) error {
	fd, err := os.Open(path)
	if err != nil {
		return errors.New("missing configuration, please run init first")
	}
	defer fd.Close()
	decoder := yaml.NewDecoder(fd)
	if err := decoder.Decode(config); err != nil {
		return fmt.Errorf("config read: %w", err)
	}
	return nil
}

func (config *ServiceConfig) WriteToFile(path string) error {
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
