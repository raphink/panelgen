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

type LoadWarning struct {
	File    string
	Line    int
	Message string
}

func (w LoadWarning) String() string {
	if w.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", w.File, w.Line, w.Message)
	}
	return fmt.Sprintf("%s: %s", w.File, w.Message)
}

// Load reads a panelgen YAML config file, recursively merging any imports.
func Load(path string) (*Config, error) {
	cfg, _, err := LoadWithWarnings(path)
	return cfg, err
}

// LoadWithWarnings reads a panelgen YAML config file and returns non-fatal
// warnings for unknown fields in that file or any imported config.
func LoadWithWarnings(path string) (*Config, []LoadWarning, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve path %s: %w", path, err)
	}
	return load(abs, map[string]bool{})
}

func load(absPath string, seen map[string]bool) (*Config, []LoadWarning, error) {
	if seen[absPath] {
		return nil, nil, fmt.Errorf("import cycle detected: %s", absPath)
	}
	seen[absPath] = true

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open config: %w", err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, nil, fmt.Errorf("parse config: %w", err)
	}
	warnings := collectUnknownFieldWarnings(absPath, &root)

	cfg := &Config{
		Scenes:     make(map[string]Scene),
		Characters: make(map[string]Character),
	}
	if err := root.Decode(cfg); err != nil {
		return nil, nil, fmt.Errorf("parse config: %w", err)
	}

	dir := filepath.Dir(absPath)
	base, importWarnings, err := mergeImports(cfg.Imports, dir, seen)
	if err != nil {
		return nil, nil, err
	}
	warnings = append(warnings, importWarnings...)

	// cfg overrides base: merge base into cfg (fills only cfg's zero-value fields).
	if err := mergo.Merge(cfg, base); err != nil {
		return nil, nil, fmt.Errorf("merge config: %w", err)
	}

	applyConfigDefaults(cfg)
	return cfg, warnings, nil
}

func mergeImports(imports []string, dir string, seen map[string]bool) (*Config, []LoadWarning, error) {
	base := &Config{
		Scenes:     make(map[string]Scene),
		Characters: make(map[string]Character),
	}
	var warnings []LoadWarning
	for _, imp := range imports {
		impPath := imp
		if !filepath.IsAbs(impPath) {
			impPath = filepath.Join(dir, imp)
		}
		impAbs, err := filepath.Abs(impPath)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve import %s: %w", imp, err)
		}
		imported, importWarnings, err := load(impAbs, seen)
		if err != nil {
			return nil, nil, fmt.Errorf("import %s: %w", imp, err)
		}
		warnings = append(warnings, importWarnings...)
		if err := mergo.Merge(base, imported); err != nil {
			return nil, nil, fmt.Errorf("merge import %s: %w", imp, err)
		}
	}
	return base, warnings, nil
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

var (
	topLevelFields  = fieldSet("imports", "style", "output_dir", "defaults", "scenes", "characters", "panels")
	defaultFields   = fieldSet("size", "quality", "assemble", "characters_dir", "characters_preprompt")
	characterFields = fieldSet(
		"prompt",
		"refs",
	)
	sceneFields = fieldSet(
		"description",
		"prompt_prefix",
		"characters",
		"refs",
		"size",
		"quality",
		"vars",
	)
	panelFields = fieldSet(
		"page",
		"scene",
		"characters",
		"prompt",
		"refs",
		"continue",
		"vars",
		"selected",
	)
)

func fieldSet(fields ...string) map[string]bool {
	out := make(map[string]bool, len(fields))
	for _, field := range fields {
		out[field] = true
	}
	return out
}

func collectUnknownFieldWarnings(file string, root *yaml.Node) []LoadWarning {
	doc := yamlDocument(root)
	if doc == nil || doc.Kind != yaml.MappingNode {
		return nil
	}

	var warnings []LoadWarning
	warnUnknownMappingFields(file, doc, "config", topLevelFields, &warnings)
	for _, pair := range mappingPairs(doc) {
		switch pair.key.Value {
		case "defaults":
			warnUnknownMappingFields(file, pair.val, "defaults", defaultFields, &warnings)
		case "characters":
			warnNamedMappingValues(file, pair.val, "characters", characterFields, &warnings)
		case "scenes":
			warnNamedMappingValues(file, pair.val, "scenes", sceneFields, &warnings)
		case "panels":
			warnPanelValues(file, pair.val, &warnings)
		}
	}
	return warnings
}

func yamlDocument(root *yaml.Node) *yaml.Node {
	if root == nil {
		return nil
	}
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		return root.Content[0]
	}
	return root
}

type yamlPair struct {
	key *yaml.Node
	val *yaml.Node
}

func mappingPairs(node *yaml.Node) []yamlPair {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	pairs := make([]yamlPair, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		pairs = append(pairs, yamlPair{key: node.Content[i], val: node.Content[i+1]})
	}
	return pairs
}

func warnUnknownMappingFields(file string, node *yaml.Node, context string, allowed map[string]bool, warnings *[]LoadWarning) {
	for _, pair := range mappingPairs(node) {
		if allowed[pair.key.Value] {
			continue
		}
		*warnings = append(*warnings, LoadWarning{
			File:    file,
			Line:    pair.key.Line,
			Message: fmt.Sprintf("unknown field %q in %s", pair.key.Value, context),
		})
	}
}

func warnNamedMappingValues(file string, node *yaml.Node, context string, allowed map[string]bool, warnings *[]LoadWarning) {
	for _, pair := range mappingPairs(node) {
		warnUnknownMappingFields(file, pair.val, context+"."+pair.key.Value, allowed, warnings)
	}
}

func warnPanelValues(file string, node *yaml.Node, warnings *[]LoadWarning) {
	if node == nil || node.Kind != yaml.SequenceNode {
		return
	}
	for i, panel := range node.Content {
		warnUnknownMappingFields(file, panel, fmt.Sprintf("panels[%d]", i), panelFields, warnings)
	}
}
