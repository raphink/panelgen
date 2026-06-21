package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeYAML(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_Basic(t *testing.T) {
	dir := t.TempDir()
	path := writeYAML(t, dir, "p.yml", `
panels:
  - page: 1
    prompt: hello
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Panels) != 1 {
		t.Errorf("expected 1 panel, got %d", len(cfg.Panels))
	}
	// defaults filled in
	if cfg.Defaults.Size != "1024x1024" {
		t.Errorf("default size: %q", cfg.Defaults.Size)
	}
	if cfg.Defaults.Quality != "high" {
		t.Errorf("default quality: %q", cfg.Defaults.Quality)
	}
	if cfg.OutputDir != "generated" {
		t.Errorf("output_dir: %q", cfg.OutputDir)
	}
}

func TestLoad_Import_MergesCharactersAndScenes(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "base.yml", `
characters:
  fox:
    description: a clockwork fox
scenes:
  space:
    prompt_prefix: "Space setting."
`)
	path := writeYAML(t, dir, "project.yml", `
imports:
  - base.yml
panels:
  - page: 1
    prompt: hello
    scene: space
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Characters["fox"]; !ok {
		t.Error("expected imported character 'fox'")
	}
	if _, ok := cfg.Scenes["space"]; !ok {
		t.Error("expected imported scene 'space'")
	}
}

func TestLoad_Import_ProjectOverridesBase(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "base.yml", `
scenes:
  space:
    prompt_prefix: "Base prefix."
`)
	path := writeYAML(t, dir, "project.yml", `
imports:
  - base.yml
scenes:
  space:
    prompt_prefix: "Project prefix."
panels:
  - page: 1
    prompt: hello
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Scenes["space"].PromptPrefix != "Project prefix." {
		t.Errorf("expected project to override base, got %q", cfg.Scenes["space"].PromptPrefix)
	}
}

func TestLoad_Import_ChainedImports(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "grandparent.yml", `
characters:
  wolf:
    description: a wolf
`)
	writeYAML(t, dir, "parent.yml", `
imports:
  - grandparent.yml
characters:
  fox:
    description: a fox
`)
	path := writeYAML(t, dir, "project.yml", `
imports:
  - parent.yml
panels:
  - page: 1
    prompt: hello
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Characters["wolf"]; !ok {
		t.Error("expected transitively imported character 'wolf'")
	}
	if _, ok := cfg.Characters["fox"]; !ok {
		t.Error("expected imported character 'fox'")
	}
}

func TestLoad_Import_DoesNotInheritPanels(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "base.yml", `
panels:
  - page: 99
    prompt: should not appear
`)
	path := writeYAML(t, dir, "project.yml", `
imports:
  - base.yml
panels:
  - page: 1
    prompt: hello
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Panels) != 1 || cfg.Panels[0].Page != 1 {
		t.Errorf("panels should not be inherited from imports, got %v", cfg.Panels)
	}
}

func TestLoad_Import_CycleDetected(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "a.yml", `imports: [b.yml]`)
	writeYAML(t, dir, "b.yml", `imports: [a.yml]`)
	_, err := Load(filepath.Join(dir, "a.yml"))
	if err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestLoad_Import_StyleInherited(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "base.yml", `style: shared/style.txt`)
	path := writeYAML(t, dir, "project.yml", `
imports:
  - base.yml
panels:
  - page: 1
    prompt: hello
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Style != "shared/style.txt" {
		t.Errorf("expected inherited style, got %q", cfg.Style)
	}
}

func TestLoad_Import_StyleOverridden(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "base.yml", `style: base/style.txt`)
	path := writeYAML(t, dir, "project.yml", `
imports:
  - base.yml
style: project/style.txt
panels:
  - page: 1
    prompt: hello
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Style != "project/style.txt" {
		t.Errorf("expected project style to win, got %q", cfg.Style)
	}
}

func TestLoad_Import_DefaultsInherited(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "base.yml", `
defaults:
  size: 1536x1024
  quality: medium
`)
	path := writeYAML(t, dir, "project.yml", `
imports:
  - base.yml
panels:
  - page: 1
    prompt: hello
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Defaults.Size != "1536x1024" {
		t.Errorf("expected inherited size, got %q", cfg.Defaults.Size)
	}
	if cfg.Defaults.Quality != "medium" {
		t.Errorf("expected inherited quality, got %q", cfg.Defaults.Quality)
	}
}

func TestLoad_Import_DefaultsOverridden(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "base.yml", `
defaults:
  size: 1536x1024
  quality: medium
`)
	path := writeYAML(t, dir, "project.yml", `
imports:
  - base.yml
defaults:
  quality: low
panels:
  - page: 1
    prompt: hello
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Defaults.Size != "1536x1024" {
		t.Errorf("expected inherited size, got %q", cfg.Defaults.Size)
	}
	if cfg.Defaults.Quality != "low" {
		t.Errorf("expected project quality to win, got %q", cfg.Defaults.Quality)
	}
}

func TestLoad_Import_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path := writeYAML(t, dir, "project.yml", `imports: [nonexistent.yml]`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing import")
	}
}

func TestLoad_Import_AbsolutePath(t *testing.T) {
	dir := t.TempDir()
	basePath := writeYAML(t, dir, "base.yml", `
characters:
  fox:
    description: a fox
`)
	path := writeYAML(t, dir, "project.yml", "imports:\n  - "+basePath+"\npanels:\n  - page: 1\n    prompt: hi\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Characters["fox"]; !ok {
		t.Error("expected character from absolute-path import")
	}
}
