package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/raphink/panelgen/internal/config"
	"github.com/raphink/panelgen/internal/generate"
	"github.com/raphink/panelgen/internal/ui"
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

func planIdx(idx, total int) string {
	w := fmt.Sprintf("%d", total)
	return ui.Dim(fmt.Sprintf("[%*d/%s]", len(w), idx, w))
}

func printPlanResult(result panelPlanResult, panel config.Panel, idx, total int, showRefs, showPrompt bool, planned, skipped, invalid int) (int, int, int) {
	pageLabel := ui.Bold(fmt.Sprintf("Page %d", panel.Page))
	scene := ""
	if panel.Scene != "" {
		scene = ui.Sep() + ui.Dim(panel.Scene)
	}
	switch result.status {
	case "skip":
		fmt.Fprintf(os.Stdout, "%s %s %s%s%s\n",
			planIdx(idx, total), ui.IconSkip, pageLabel, scene, ui.Dim(" — "+result.reason))
		skipped++
	case "invalid":
		fmt.Fprintf(os.Stdout, "%s %s %s%s%s\n",
			planIdx(idx, total), ui.IconFail, pageLabel, scene, ui.Dim(fmt.Sprintf(" — %v", result.err)))
		invalid++
	case "plan":
		fmt.Fprintf(os.Stdout, "%s %s %s%s\n",
			planIdx(idx, total), ui.IconPlan, pageLabel, scene)
		fmt.Fprintf(os.Stdout, "  %s %s\n",
			ui.Dim("output "), result.output)
		fmt.Fprintf(os.Stdout, "  %s %s%s%s\n",
			ui.Dim("size   "), result.size, ui.Sep(), result.quality)
		if len(result.refs) == 0 {
			fmt.Fprintf(os.Stdout, "  %s %s\n", ui.Dim("refs   "), ui.Dim("(none)"))
		} else {
			fmt.Fprintf(os.Stdout, "  %s\n", ui.Dim("refs   "))
			for _, r := range result.refs {
				fmt.Fprintf(os.Stdout, "    %s %s\n", ui.Dim("·"), r)
			}
		}
		if showPrompt {
			fmt.Fprintf(os.Stdout, "  %s\n", ui.Dim("prompt "))
			for _, line := range strings.Split(result.prompt, "\n") {
				fmt.Fprintf(os.Stdout, "    %s\n", ui.Dim(line))
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

func resolvePanelScene(panel config.Panel, cfg *config.Config, configDir string) (prefix string, refs []string, size, quality string, err error) {
	if panel.Scene == "" {
		return "", nil, "", "", nil
	}
	resolved, err := generate.ResolveScene(cfg, panel.Scene, configDir, panel.Vars)
	if err != nil {
		return "", nil, "", "", err
	}
	return resolved.Prefix, resolved.Refs, resolved.Size, resolved.Quality, nil
}

func planOnePanel(panel config.Panel, cfg *config.Config, configDir, outputDir, size, quality, styleFile string, force bool) panelPlanResult {
	prompt := strings.TrimSpace(panel.Prompt)
	if prompt == "" || panel.Scene == "blank" {
		return panelPlanResult{status: "skip", reason: "blank"}
	}

	prefix, sceneRefs, sceneSize, sceneQuality, err := resolvePanelScene(panel, cfg, configDir)
	if err != nil {
		return panelPlanResult{status: "invalid", err: err}
	}

	finalSize := firstNonEmpty(size, sceneSize, cfg.Defaults.Size, "1024x1024")
	finalQuality := firstNonEmpty(quality, sceneQuality, cfg.Defaults.Quality, "high")

	panelCharDescs, panelCharRefs := generate.ResolveCharacters(cfg, panel.Characters, configDir)
	prefix = generate.MergeCharPrefix(prefix, panelCharDescs)

	var contRefs []string
	if panel.Continue > 0 {
		if img := generate.BestPageImage(outputDir, panel.Continue); img != "" {
			contRefs = []string{img}
		}
	}
	panelRefs := append(append(panelCharRefs, contRefs...), generate.AbsRefs(panel.Refs, configDir)...)
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
	lintContinueDeps(cfg, add)
	return issues
}

func generationPreflightIssues(cfg *config.Config, configDir, styleFlag string, noStyle bool, sizeOverride, qualityOverride string) []lintIssue {
	issues := lintConfig(cfg, configDir, styleFlag, noStyle)
	add := func(level, msg string) {
		issues = append(issues, lintIssue{level: level, msg: msg})
	}
	lintSizeQuality("override", sizeOverride, qualityOverride, add)
	lintResolvedPanelOptions(cfg, sizeOverride, qualityOverride, add)
	return issues
}

func requireNoPreflightErrors(issues []lintIssue) {
	errors := 0
	for _, issue := range issues {
		if issue.level != "error" {
			continue
		}
		fmt.Fprintf(os.Stderr, "%s %s\n", ui.IconFail, ui.Red(issue.msg))
		errors++
	}
	if errors > 0 {
		fatalf("validation failed with %d error(s); run `panelgen lint` for the full report", errors)
	}
}

func lintSizeQualityIssues(label, size, quality string) []lintIssue {
	var issues []lintIssue
	lintSizeQuality(label, size, quality, func(level, msg string) {
		issues = append(issues, lintIssue{level: level, msg: msg})
	})
	return issues
}

func lintSizeQuality(label, size, quality string, add func(string, string)) {
	if size != "" && !isValidSize(size) {
		add("error", fmt.Sprintf("%s size %q is invalid (must be WxH, both dims divisible by 16, <=8,294,400 px)", label, size))
	}
	if quality != "" && !validQualities[quality] {
		add("error", fmt.Sprintf("%s quality %q is invalid (expected one of: low, medium, high)", label, quality))
	}
}

func lintResolvedPanelOptions(cfg *config.Config, sizeOverride, qualityOverride string, add func(string, string)) {
	for i, p := range cfg.Panels {
		if strings.TrimSpace(p.Prompt) == "" || p.Scene == "blank" {
			continue
		}
		var scene config.Scene
		if p.Scene != "" {
			var ok bool
			scene, ok = cfg.Scenes[p.Scene]
			if !ok {
				continue
			}
		}
		size := firstNonEmpty(sizeOverride, scene.Size, cfg.Defaults.Size, "1024x1024")
		quality := firstNonEmpty(qualityOverride, scene.Quality, cfg.Defaults.Quality, "high")
		lintSizeQuality(fmt.Sprintf("panel[%d] page=%d resolved", i, p.Page), size, quality, add)
	}
}

func buildContinueMap(cfg *config.Config) (pageSet map[int]bool, contOf map[int]int) {
	pageSet = make(map[int]bool, len(cfg.Panels))
	contOf = make(map[int]int)
	for _, p := range cfg.Panels {
		pageSet[p.Page] = true
		if p.Continue > 0 {
			contOf[p.Page] = p.Continue
		}
	}
	return
}

func checkContinueSanity(contOf map[int]int, pageSet map[int]bool, add func(string, string)) {
	for page, dep := range contOf {
		if page == dep {
			add("error", fmt.Sprintf("panel page=%d: continue: self-reference", page))
		} else if !pageSet[dep] {
			add("error", fmt.Sprintf("panel page=%d: continue=%d references a non-existent page", page, dep))
		}
	}
}

func checkContinueCycles(contOf map[int]int, add func(string, string)) {
	const (
		unvisited = 0
		inStack   = 1
		done      = 2
	)
	state := make(map[int]int, len(contOf))
	var dfs func(int) bool
	dfs = func(page int) bool {
		switch state[page] {
		case inStack:
			return true
		case done:
			return false
		}
		state[page] = inStack
		if dep, ok := contOf[page]; ok && dfs(dep) {
			return true
		}
		state[page] = done
		return false
	}
	for page := range contOf {
		if state[page] == unvisited && dfs(page) {
			add("error", fmt.Sprintf("panel page=%d: continue: references form a cycle", page))
		}
	}
}

func lintContinueDeps(cfg *config.Config, add func(string, string)) {
	pageSet, contOf := buildContinueMap(cfg)
	checkContinueSanity(contOf, pageSet, add)
	checkContinueCycles(contOf, add)
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
	seenPages := map[int]int{}
	for i, p := range cfg.Panels {
		if p.Page <= 0 {
			add("error", fmt.Sprintf("panel[%d] has invalid page number: %d", i, p.Page))
		} else if first, ok := seenPages[p.Page]; ok {
			add("error", fmt.Sprintf("panel[%d] duplicates page %d already used by panel[%d]", i, p.Page, first))
		} else {
			seenPages[p.Page] = i
		}
		if p.Scene != "" && p.Scene != "blank" {
			if _, ok := cfg.Scenes[p.Scene]; !ok {
				add("error", fmt.Sprintf("panel[%d] references unknown scene %q", i, p.Scene))
			}
		}
		for _, charName := range p.Characters {
			if _, ok := cfg.Characters[charName]; !ok {
				add("error", fmt.Sprintf("panel[%d] references unknown character %q", i, charName))
			}
		}
		if p.Continue < 0 {
			add("error", fmt.Sprintf("panel[%d] has invalid continue page: %d", i, p.Continue))
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
