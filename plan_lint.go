package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/raphink/panelgen/internal/config"
	"github.com/raphink/panelgen/internal/generate"
)

// isValidSize checks gpt-image-2 size constraints:
// both dimensions divisible by 16, total pixels <= 8,294,400.
func isValidSize(s string) bool {
	var w, h int
	if _, err := fmt.Sscanf(s, "%dx%d", &w, &h); err != nil || w <= 0 || h <= 0 {
		return false
	}
	return w%16 == 0 && h%16 == 0 && w*h <= 8_294_400
}

var validQualities = map[string]bool{
	"low":    true,
	"medium": true,
	"high":   true,
}

type lintIssue struct {
	level string
	msg   string
}

func filterPanelsByPage(panels []config.Panel, pagesFlag string) []config.Panel {
	if pagesFlag == "" {
		return panels
	}
	pageList, err := parsePageSpec(pagesFlag)
	if err != nil {
		fatalf("parse --pages: %v", err)
	}
	pageSet := make(map[int]bool, len(pageList))
	for _, p := range pageList {
		pageSet[p] = true
	}
	filtered := panels[:0]
	for _, p := range panels {
		if pageSet[p.Page] {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

func printPlanResult(result panelPlanResult, panel config.Panel, idx, total int, showRefs, showPrompt bool, planned, skipped, invalid int) (int, int, int) {
	switch result.status {
	case "skip":
		fmt.Fprintf(os.Stdout, "[%d/%d] page=%d scene=%s status=skip (%s)\n", idx, total, panel.Page, panel.Scene, result.reason)
		skipped++
	case "invalid":
		fmt.Fprintf(os.Stdout, "[%d/%d] page=%d scene=%s status=invalid (%v)\n", idx, total, panel.Page, panel.Scene, result.err)
		invalid++
	case "plan":
		fmt.Fprintf(os.Stdout, "[%d/%d] page=%d scene=%s status=plan\n", idx, total, panel.Page, panel.Scene)
		fmt.Fprintf(os.Stdout, "  output : %s\n  size   : %s\n  quality: %s\n",
			result.output, result.size, result.quality)
		if len(result.refs) == 0 {
			fmt.Fprintln(os.Stdout, "  refs   : (none)")
		} else {
			fmt.Fprintln(os.Stdout, "  refs   :")
			for _, r := range result.refs {
				fmt.Fprintf(os.Stdout, "    - %s\n", r)
			}
		}
		if showPrompt {
			fmt.Fprintln(os.Stdout, "  prompt:")
			for _, line := range strings.Split(result.prompt, "\n") {
				fmt.Fprintf(os.Stdout, "    %s\n", line)
			}
		}
		planned++
	}
	return planned, skipped, invalid
}

type panelPlanResult struct {
	status  string
	reason  string
	err     error
	output  string
	size    string
	quality string
	refs    []string
	prompt  string
}

func planOnePanel(panel config.Panel, cfg *config.Config, configDir, outputDir, size, quality, styleFile string, force bool) panelPlanResult {
	prompt := strings.TrimSpace(panel.Prompt)
	if prompt == "" || panel.Scene == "blank" {
		return panelPlanResult{status: "skip", reason: "blank"}
	}

	prefix := ""
	var sceneRefs []string
	sceneSize, sceneQuality := "", ""
	if panel.Scene != "" {
		resolved, err := generate.ResolveScene(cfg, panel.Scene, configDir, panel.Vars)
		if err != nil {
			return panelPlanResult{status: "invalid", err: err}
		}
		prefix = resolved.Prefix
		sceneRefs = resolved.Refs
		sceneSize = resolved.Size
		sceneQuality = resolved.Quality
	}

	finalSize := firstNonEmpty(size, sceneSize, cfg.Defaults.Size, "1024x1024")
	finalQuality := firstNonEmpty(quality, sceneQuality, cfg.Defaults.Quality, "high")

	panelCharDescs, panelCharRefs := generate.ResolveCharacters(cfg, panel.Characters, configDir)
	if len(panelCharDescs) > 0 {
		extra := strings.Join(panelCharDescs, "\n\n")
		if prefix != "" {
			prefix = prefix + "\n\n" + extra
		} else {
			prefix = extra
		}
	}

	var panelRefs []string
	panelRefs = append(panelRefs, panelCharRefs...)
	if panel.Continue > 0 {
		if img := generate.BestPageImage(outputDir, panel.Continue); img != "" {
			panelRefs = append(panelRefs, img)
		}
	}
	for _, r := range panel.Refs {
		path := r
		if !filepath.IsAbs(path) {
			path = filepath.Join(configDir, r)
		}
		panelRefs = append(panelRefs, path)
	}
	allRefs := append(sceneRefs, panelRefs...)

	if !force && generate.HasVersion(outputDir, panel.Page, finalQuality) {
		return panelPlanResult{status: "skip", reason: finalQuality + " exists"}
	}

	output := generate.NextVersion(outputDir, panel.Page, finalQuality)
	fullPrompt, err := generate.BuildPrompt(prompt, styleFile, prefix)
	if err != nil {
		return panelPlanResult{status: "invalid", err: err}
	}

	return panelPlanResult{
		status:  "plan",
		output:  output,
		size:    finalSize,
		quality: finalQuality,
		refs:    allRefs,
		prompt:  fullPrompt,
	}
}

func lintConfig(cfg *config.Config, configDir, styleFlag string, noStyle bool) []lintIssue {
	var issues []lintIssue
	add := func(level, msg string) {
		issues = append(issues, lintIssue{level: level, msg: msg})
	}
	lintDefaults(cfg, add)
	lintStyleCheck(cfg, configDir, styleFlag, noStyle, add)
	lintCharacters(cfg, configDir, add)
	lintScenes(cfg, configDir, add)
	lintPanels(cfg, configDir, add)
	return issues
}

func lintDefaults(cfg *config.Config, add func(string, string)) {
	if cfg.Defaults.Size != "" && !isValidSize(cfg.Defaults.Size) {
		add("warning", fmt.Sprintf("defaults.size %q is invalid (must be WxH, both dims divisible by 16, <=8,294,400 px)", cfg.Defaults.Size))
	}
	if cfg.Defaults.Quality != "" && !validQualities[cfg.Defaults.Quality] {
		add("warning", fmt.Sprintf("defaults.quality %q is non-standard", cfg.Defaults.Quality))
	}
}

func lintStyleCheck(cfg *config.Config, configDir, styleFlag string, noStyle bool, add func(string, string)) {
	if noStyle {
		return
	}
	stylePath := styleFlag
	if stylePath == "" {
		stylePath = cfg.Style
	}
	if stylePath == "" {
		return
	}
	if !filepath.IsAbs(stylePath) {
		stylePath = filepath.Join(configDir, stylePath)
	}
	if _, err := os.Stat(stylePath); err != nil {
		add("warning", fmt.Sprintf("style file not found: %s", stylePath))
	}
}

func lintCharacters(cfg *config.Config, configDir string, add func(string, string)) {
	for name, c := range cfg.Characters {
		lintRefs(c.Refs, configDir, fmt.Sprintf("character %q", name), add)
	}
}

func lintScenes(cfg *config.Config, configDir string, add func(string, string)) {
	sceneNames := make([]string, 0, len(cfg.Scenes))
	for name := range cfg.Scenes {
		sceneNames = append(sceneNames, name)
	}
	sort.Strings(sceneNames)
	for _, name := range sceneNames {
		s := cfg.Scenes[name]
		if s.Size != "" && !isValidSize(s.Size) {
			add("warning", fmt.Sprintf("scene %q size %q is invalid (must be WxH, both dims divisible by 16, <=8,294,400 px)", name, s.Size))
		}
		if s.Quality != "" && !validQualities[s.Quality] {
			add("warning", fmt.Sprintf("scene %q quality %q is non-standard", name, s.Quality))
		}
		for _, charName := range s.Characters {
			if _, ok := cfg.Characters[charName]; !ok {
				add("error", fmt.Sprintf("scene %q references unknown character %q", name, charName))
			}
		}
		lintRefs(s.Refs, configDir, fmt.Sprintf("scene %q", name), add)
	}
}

func lintPanels(cfg *config.Config, configDir string, add func(string, string)) {
	if len(cfg.Panels) == 0 {
		add("error", "no panels defined")
		return
	}
	for i, p := range cfg.Panels {
		if p.Page <= 0 {
			add("error", fmt.Sprintf("panel[%d] has invalid page number: %d", i, p.Page))
		}
		if p.Scene != "" && p.Scene != "blank" {
			if _, ok := cfg.Scenes[p.Scene]; !ok {
				add("error", fmt.Sprintf("panel[%d] references unknown scene %q", i, p.Scene))
			}
		}
		if strings.TrimSpace(p.Prompt) == "" && p.Scene != "blank" {
			add("warning", fmt.Sprintf("panel[%d] has empty prompt", i))
		}
		lintRefs(p.Refs, configDir, fmt.Sprintf("panel[%d]", i), add)
	}
}

func lintRefs(refs []string, configDir, label string, add func(string, string)) {
	for _, r := range refs {
		path := r
		if !filepath.IsAbs(path) {
			path = filepath.Join(configDir, path)
		}
		if _, err := os.Stat(path); err != nil {
			add("warning", fmt.Sprintf("%s ref not found: %s", label, path))
		}
	}
}

func mustLoadConfig(configFile string) (*config.Config, string) {
	if _, err := os.Stat(configFile); err != nil {
		fatalf("config file not found: %s", configFile)
	}
	cfg, err := config.Load(configFile)
	if err != nil {
		fatalf("load config: %v", err)
	}
	return cfg, filepath.Dir(configFile)
}
