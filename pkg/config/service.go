package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
)

type ServiceConfig struct {
	Project     string   `yaml:"project,omitempty"`
	Service     string   `yaml:"service,omitempty"`
	Constructor string   `yaml:"constructor,omitempty"`
	Ignore      []string `yaml:"ignore"`
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
