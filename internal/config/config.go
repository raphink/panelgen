// Package config loads and validates panelgen YAML configuration files.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the top-level panelgen configuration.
type Config struct {
	Imports    []string             `yaml:"imports"`
	Style      string               `yaml:"style"`
	OutputDir  string               `yaml:"output_dir"`
	Defaults   Defaults             `yaml:"defaults"`
	Scenes     map[string]Scene     `yaml:"scenes"`
	Characters map[string]Character `yaml:"characters"`
	Panels     []Panel              `yaml:"panels"`
}

type Defaults struct {
	Size     string `yaml:"size"`
	Quality  string `yaml:"quality"`
	Assemble *bool  `yaml:"assemble"`
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
	Page       int      `yaml:"page"`
	Scene      string   `yaml:"scene"`
	Characters []string `yaml:"characters"`
	Prompt     string   `yaml:"prompt"`
	Refs       []string `yaml:"refs"`
	Selected   string   `yaml:"selected"`
}

// Load reads a panelgen YAML config file, recursively merging any imports.
func Load(path string) (*Config, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve path %s: %w", path, err)
	}
	return load(abs, map[string]bool{})
}

func load(absPath string, seen map[string]bool) (*Config, error) {
	if seen[absPath] {
		return nil, fmt.Errorf("import cycle detected: %s", absPath)
	}
	seen[absPath] = true

	data, err := os.ReadFile(absPath)
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

	// Process imports first, then let cfg's own definitions override.
	base := &Config{
		Scenes:     make(map[string]Scene),
		Characters: make(map[string]Character),
	}
	dir := filepath.Dir(absPath)
	for _, imp := range cfg.Imports {
		impPath := imp
		if !filepath.IsAbs(impPath) {
			impPath = filepath.Join(dir, imp)
		}
		impAbs, err := filepath.Abs(impPath)
		if err != nil {
			return nil, fmt.Errorf("resolve import %s: %w", imp, err)
		}
		imported, err := load(impAbs, seen)
		if err != nil {
			return nil, fmt.Errorf("import %s: %w", imp, err)
		}
		if base.Style == "" {
			base.Style = imported.Style
		}
		if base.Defaults.Size == "" {
			base.Defaults.Size = imported.Defaults.Size
		}
		if base.Defaults.Quality == "" {
			base.Defaults.Quality = imported.Defaults.Quality
		}
		if base.Defaults.Assemble == nil {
			base.Defaults.Assemble = imported.Defaults.Assemble
		}
		for k, v := range imported.Characters {
			base.Characters[k] = v
		}
		for k, v := range imported.Scenes {
			base.Scenes[k] = v
		}
	}

	// Merge: cfg overrides base.
	if cfg.Style == "" {
		cfg.Style = base.Style
	}
	if cfg.Defaults.Size == "" {
		cfg.Defaults.Size = base.Defaults.Size
	}
	if cfg.Defaults.Quality == "" {
		cfg.Defaults.Quality = base.Defaults.Quality
	}
	if cfg.Defaults.Assemble == nil {
		cfg.Defaults.Assemble = base.Defaults.Assemble
	}
	for k, v := range cfg.Characters {
		base.Characters[k] = v
	}
	for k, v := range cfg.Scenes {
		base.Scenes[k] = v
	}
	cfg.Characters = base.Characters
	cfg.Scenes = base.Scenes

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
