package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Project struct {
	ID    string `yaml:"id"`
	Label string `yaml:"label"`
}

type Config struct {
	Defaults struct {
		Namespace               string `yaml:"namespace"`
		ReconnectRetries        int    `yaml:"reconnect_retries"`
		ReconnectBackoffSeconds int    `yaml:"reconnect_backoff_seconds"`
	} `yaml:"defaults"`
	Projects      []Project      `yaml:"projects"`
	PortOverrides   map[string]int `yaml:"port_overrides"`
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".kpm", "config.yaml"), nil
}

func Load() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cfg := defaultConfig
		if err := Save(&cfg); err != nil {
			return nil, err
		}
		return &cfg, nil
	}
	if err != nil {
		return nil, err
	}

	cfg := defaultConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func Save(c *Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
