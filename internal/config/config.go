// Package config loads and validates panelgen YAML configuration files.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the top-level panelgen configuration.
type Config struct {
	Style      string               `yaml:"style"`
	OutputDir  string               `yaml:"output_dir"`
	Defaults   Defaults             `yaml:"defaults"`
	Scenes     map[string]Scene     `yaml:"scenes"`
	Characters map[string]Character `yaml:"characters"`
	Panels     []Panel              `yaml:"panels"`
}

type Defaults struct {
	Size    string `yaml:"size"`
	Quality string `yaml:"quality"`
}

type Character struct {
	Description string   `yaml:"description"`
	Refs        []string `yaml:"refs"`
}

type Scene struct {
	Description  string   `yaml:"description"`
	PromptPrefix string   `yaml:"prompt_prefix"`
	Characters   []string `yaml:"characters"`
	Refs         []string `yaml:"refs"`
	Size         string   `yaml:"size"`
	Quality      string   `yaml:"quality"`
}

type Panel struct {
	Page     int      `yaml:"page"`
	Scene    string   `yaml:"scene"`
	Prompt   string   `yaml:"prompt"`
	Refs     []string `yaml:"refs"`
	Selected string   `yaml:"selected"`
}

// Load reads a panelgen YAML config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}

	cfg := &Config{
		Scenes:     make(map[string]Scene),
		Characters: make(map[string]Character),
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.Defaults.Size == "" {
		cfg.Defaults.Size = "1024x1024"
	}
	if cfg.Defaults.Quality == "" {
		cfg.Defaults.Quality = "high"
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = "generated"
	}

	return cfg, nil
}
