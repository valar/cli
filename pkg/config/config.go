package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
)

type Config struct {
	Project     string `yaml:"project"`
	Function    string `yaml:"function"`
	Environment struct {
		Name string `yaml:"name"`
	} `yaml:"environment"`
	Ignore []string `yaml:"ignore"`
}

func (config *Config) ReadFromFile(path string) error {
	fd, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("config read: %w", err)
	}
	defer fd.Close()
	decoder := yaml.NewDecoder(fd)
	if err := decoder.Decode(config); err != nil {
		return fmt.Errorf("config read: %w", err)
	}
	return nil
}

func (config *Config) WriteToFile(path string) error {
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
