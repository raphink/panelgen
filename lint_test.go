package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/raphink/panelgen/internal/config"
)

// ─── isValidSize ─────────────────────────────────────────────────────────────

func TestIsValidSize(t *testing.T) {
	cases := []struct {
		input string
		valid bool
	}{
		{"1024x1024", true},
		{"1024x1536", true},
		{"1536x1024", true},
		{"1920x1088", true},  // both divisible by 16, 2,088,960 px
		{"2880x2880", true},  // 8,294,400 px — exactly at limit
		{"2896x2880", false}, // 8,340,480 px — over limit
		{"1000x1000", false}, // not divisible by 16
		{"1024x1000", false}, // height not divisible by 16
		{"0x1024", false},
		{"abc", false},
		{"", false},
	}
	for _, c := range cases {
		got := isValidSize(c.input)
		if got != c.valid {
			t.Errorf("isValidSize(%q) = %v, want %v", c.input, got, c.valid)
		}
	}
}

// ─── lintConfig ──────────────────────────────────────────────────────────────

func TestLintConfig_NoPanels(t *testing.T) {
	cfg := &config.Config{
		Scenes:     map[string]config.Scene{},
		Characters: map[string]config.Character{},
	}
	issues := lintConfig(cfg, ".", "", true)
	assertIssue(t, issues, "error", "no panels defined")
}

func TestLintConfig_InvalidDefaultSize(t *testing.T) {
	cfg := minimalConfig()
	cfg.Defaults.Size = "999x999"
	issues := lintConfig(cfg, ".", "", true)
	assertIssue(t, issues, "warning", "defaults.size")
}

func TestLintConfig_InvalidDefaultQuality(t *testing.T) {
	cfg := minimalConfig()
	cfg.Defaults.Quality = "ultra"
	issues := lintConfig(cfg, ".", "", true)
	assertIssue(t, issues, "warning", "defaults.quality")
}

func TestLintConfig_UnknownSceneRef(t *testing.T) {
	cfg := minimalConfig()
	cfg.Panels[0].Scene = "nonexistent"
	issues := lintConfig(cfg, ".", "", true)
	assertIssue(t, issues, "error", "unknown scene")
}

func TestLintConfig_UnknownCharacterInScene(t *testing.T) {
	cfg := minimalConfig()
	cfg.Scenes["s"] = config.Scene{Characters: []string{"ghost"}}
	cfg.Panels[0].Scene = "s"
	issues := lintConfig(cfg, ".", "", true)
	assertIssue(t, issues, "error", "unknown character")
}

func TestLintConfig_MissingCharacterRef(t *testing.T) {
	cfg := minimalConfig()
	cfg.Characters["fox"] = config.Character{Refs: []string{"missing.png"}}
	issues := lintConfig(cfg, ".", "", true)
	assertIssue(t, issues, "warning", "ref not found")
}

func TestLintConfig_InvalidSceneSize(t *testing.T) {
	cfg := minimalConfig()
	cfg.Scenes["s"] = config.Scene{Size: "999x999"}
	cfg.Panels[0].Scene = "s"
	issues := lintConfig(cfg, ".", "", true)
	assertIssue(t, issues, "warning", "size")
}

func TestLintConfig_StyleFileChecked(t *testing.T) {
	cfg := minimalConfig()
	issues := lintConfig(cfg, ".", "/nonexistent/style.txt", false)
	assertIssue(t, issues, "warning", "style file not found")
}

func TestLintConfig_StyleFileFound(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "style*.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	cfg := minimalConfig()
	issues := lintConfig(cfg, ".", f.Name(), false)
	for _, i := range issues {
		if i.level == "warning" && contains(i.msg, "style") {
			t.Errorf("unexpected style warning: %s", i.msg)
		}
	}
}

func TestLintConfig_NoStyleWhenNoStyle(t *testing.T) {
	cfg := minimalConfig()
	cfg.Style = "/nonexistent/style.txt"
	// noStyle=true should suppress style check entirely
	issues := lintConfig(cfg, ".", "", true)
	for _, i := range issues {
		if contains(i.msg, "style") {
			t.Errorf("unexpected style issue when noStyle=true: %s", i.msg)
		}
	}
}

func TestLintConfig_PanelEmptyPrompt(t *testing.T) {
	cfg := minimalConfig()
	cfg.Panels[0].Prompt = "   "
	cfg.Panels[0].Scene = "" // not blank
	issues := lintConfig(cfg, ".", "", true)
	assertIssue(t, issues, "warning", "empty prompt")
}

func TestLintConfig_PanelInvalidPage(t *testing.T) {
	cfg := minimalConfig()
	cfg.Panels[0].Page = 0
	issues := lintConfig(cfg, ".", "", true)
	assertIssue(t, issues, "error", "invalid page number")
}

func TestLintConfig_PanelMissingRef(t *testing.T) {
	cfg := minimalConfig()
	cfg.Panels[0].Refs = []string{"missing.png"}
	issues := lintConfig(cfg, ".", "", true)
	assertIssue(t, issues, "warning", "ref not found")
}

func TestLintConfig_PanelBlankSkipsPromptCheck(t *testing.T) {
	cfg := minimalConfig()
	cfg.Panels[0].Scene = "blank"
	cfg.Panels[0].Prompt = ""
	issues := lintConfig(cfg, ".", "", true)
	for _, i := range issues {
		if contains(i.msg, "empty prompt") {
			t.Errorf("blank panel should not warn about empty prompt: %s", i.msg)
		}
	}
}

// ─── filterPanelsByPage ───────────────────────────────────────────────────────

func TestFilterPanelsByPage_NoFilter(t *testing.T) {
	panels := []config.Panel{{Page: 1}, {Page: 2}}
	got := filterPanelsByPage(panels, "")
	if len(got) != 2 {
		t.Errorf("expected 2, got %d", len(got))
	}
}

func TestFilterPanelsByPage_Range(t *testing.T) {
	panels := []config.Panel{{Page: 1}, {Page: 2}, {Page: 3}, {Page: 4}}
	got := filterPanelsByPage(panels, "1,3-4")
	if len(got) != 3 {
		t.Errorf("expected 3, got %d: %v", len(got), got)
	}
}

// ─── applySelectedOverrides ──────────────────────────────────────────────────

func TestApplySelectedOverrides_Override(t *testing.T) {
	dir := t.TempDir()
	custom := "page_1_high-2.png"
	if err := os.WriteFile(filepath.Join(dir, custom), nil, 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Panels: []config.Panel{{Page: 1, Selected: custom}},
	}
	best := []pageCandidate{{page: 1, path: filepath.Join(dir, "page_1_high-1.png")}}
	got := applySelectedOverrides(cfg, dir, best)
	if len(got) != 1 || filepath.Base(got[0].path) != custom {
		t.Errorf("expected override to %s, got %v", custom, got)
	}
}

func TestApplySelectedOverrides_MissingSelected(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Panels: []config.Panel{{Page: 1, Selected: "nonexistent.png"}},
	}
	orig := filepath.Join(dir, "page_1_high-1.png")
	best := []pageCandidate{{page: 1, path: orig}}
	got := applySelectedOverrides(cfg, dir, best)
	// should fall back to auto
	if len(got) != 1 || got[0].path != orig {
		t.Errorf("expected fallback to %s, got %v", orig, got)
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func minimalConfig() *config.Config {
	return &config.Config{
		Scenes:     map[string]config.Scene{},
		Characters: map[string]config.Character{},
		Panels:     []config.Panel{{Page: 1, Prompt: "a prompt"}},
	}
}

func assertIssue(t *testing.T, issues []lintIssue, level, substr string) {
	t.Helper()
	for _, i := range issues {
		if i.level == level && contains(i.msg, substr) {
			return
		}
	}
	t.Errorf("expected %s issue containing %q; got: %v", level, substr, issues)
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub)))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
