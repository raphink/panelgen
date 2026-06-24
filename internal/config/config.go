// Package config loads and validates panelgen YAML configuration files.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"dario.cat/mergo"
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
	Size                string `yaml:"size"`
	Quality             string `yaml:"quality"`
	Assemble            *bool  `yaml:"assemble"`
	CharactersDir       string `yaml:"characters_dir"`
	CharactersPreprompt string `yaml:"characters_preprompt"`
}

type Character struct {
	Prompt string   `yaml:"prompt"`
	Refs   []string `yaml:"refs"`
}

type Scene struct {
	Description  string            `yaml:"description"`
	PromptPrefix string            `yaml:"prompt_prefix"`
	Characters   []string          `yaml:"characters"`
	Refs         []string          `yaml:"refs"`
	Size         string            `yaml:"size"`
	Quality      string            `yaml:"quality"`
	Vars         map[string]string `yaml:"vars"`
}

type Panel struct {
	Page       int               `yaml:"page"`
	Scene      string            `yaml:"scene"`
	Characters []string          `yaml:"characters"`
	Prompt     string            `yaml:"prompt"`
	Refs       []string          `yaml:"refs"`
	Continue   int               `yaml:"continue"`
	Vars       map[string]string `yaml:"vars"`
	Selected   string            `yaml:"selected"`
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

	dir := filepath.Dir(absPath)
	base, err := mergeImports(cfg.Imports, dir, seen)
	if err != nil {
		return nil, err
	}

	// cfg overrides base: merge base into cfg (fills only cfg's zero-value fields).
	if err := mergo.Merge(cfg, base); err != nil {
		return nil, fmt.Errorf("merge config: %w", err)
	}

	applyConfigDefaults(cfg)
	return cfg, nil
}

func mergeImports(imports []string, dir string, seen map[string]bool) (*Config, error) {
	base := &Config{
		Scenes:     make(map[string]Scene),
		Characters: make(map[string]Character),
	}
	for _, imp := range imports {
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
		if err := mergo.Merge(base, imported); err != nil {
			return nil, fmt.Errorf("merge import %s: %w", imp, err)
		}
	}
	return base, nil
}

func applyConfigDefaults(cfg *Config) {
	if cfg.Defaults.Size == "" {
		cfg.Defaults.Size = "1024x1024"
	}
	if cfg.Defaults.Quality == "" {
		cfg.Defaults.Quality = "high"
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = "generated"
	}
}
