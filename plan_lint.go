package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/raphink/panelgen/internal/config"
	"github.com/raphink/panelgen/internal/generate"
)

var validSizes = map[string]bool{
	"1024x1024": true,
	"1024x1536": true,
	"1536x1024": true,
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

func cmdLint(args []string) {
	fs := flag.NewFlagSet("lint", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: panelgen lint [options]

Validate config shape, scene/character references, and local file paths.

OPTIONS
`)
		fs.PrintDefaults()
	}

	configFile := fs.String("config", defaultConfig, "Config `FILE`")
	styleFile := fs.String("style", "", "Style guide text `FILE`")
	noStyle := fs.Bool("no-style", false, "Disable style guide checks")
	strict := fs.Bool("strict", false, "Treat warnings as errors")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	cfg, configDir := mustLoadConfig(*configFile)
	issues := lintConfig(cfg, configDir, *styleFile, *noStyle)

	errors := 0
	warnings := 0
	for _, i := range issues {
		fmt.Fprintf(os.Stderr, "%s: %s\n", strings.ToUpper(i.level), i.msg)
		if i.level == "error" {
			errors++
		} else {
			warnings++
		}
	}

	fmt.Fprintf(os.Stderr, "\nLint summary: %d error(s), %d warning(s)\n", errors, warnings)
	if errors > 0 || (*strict && warnings > 0) {
		os.Exit(1)
	}
}

func cmdPlan(args []string) {
	fs := flag.NewFlagSet("plan", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: panelgen plan [options]

Preview batch generation: resolved outputs, refs, and prompts (optional).
No API calls are made.

OPTIONS
`)
		fs.PrintDefaults()
	}

	configFile := fs.String("config", defaultConfig, "Config `FILE`")
	styleFile := fs.String("style", "", "Style guide text `FILE`")
	noStyle := fs.Bool("no-style", false, "Disable style guide")
	size := fs.String("size", "", "Override image size for all panels")
	quality := fs.String("quality", "", "Override image quality for all panels")
	pages := fs.String("pages", "", "Page subset, e.g. '1,3,5-10,20'")
	force := fs.Bool("force", false, "Show a new version even if output exists")
	showPrompt := fs.Bool("show-prompt", false, "Show full resolved prompt per panel")
	showRefs := fs.Bool("show-refs", false, "List all resolved refs per panel")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	cfg, configDir := mustLoadConfig(*configFile)
	resolvedStyle := resolveStyle(*styleFile, *noStyle, cfg, configDir)

	if *size != "" && !validSizes[*size] {
		fatalf("invalid --size %q (expected one of: 1024x1024, 1024x1536, 1536x1024)", *size)
	}
	if *quality != "" && !validQualities[*quality] {
		fatalf("invalid --quality %q (expected one of: low, medium, high)", *quality)
	}

	panels := cfg.Panels
	if len(*pages) > 0 {
		pageList, err := parsePageSpec(*pages)
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
		panels = filtered
	}

	if len(panels) == 0 {
		fatalf("no panels to plan")
	}

	outputDir := filepath.Join(configDir, cfg.OutputDir)
	_ = os.MkdirAll(outputDir, 0755)

	total := len(panels)
	planned := 0
	skipped := 0
	invalid := 0

	for i, panel := range panels {
		idx := i + 1
		prompt := strings.TrimSpace(panel.Prompt)
		if prompt == "" || panel.Scene == "blank" {
			fmt.Fprintf(os.Stdout, "[%d/%d] page=%d scene=%s status=skip (blank)\n", idx, total, panel.Page, panel.Scene)
			skipped++
			continue
		}

		prefix := ""
		var sceneRefs []string
		sceneSize := ""
		sceneQuality := ""
		if panel.Scene != "" {
			resolved, err := generate.ResolveScene(cfg, panel.Scene, configDir)
			if err != nil {
				fmt.Fprintf(os.Stdout, "[%d/%d] page=%d scene=%s status=invalid (%v)\n", idx, total, panel.Page, panel.Scene, err)
				invalid++
				continue
			}
			prefix = resolved.Prefix
			sceneRefs = resolved.Refs
			sceneSize = resolved.Size
			sceneQuality = resolved.Quality
		}

		finalSize := firstNonEmpty(*size, sceneSize, cfg.Defaults.Size, "1024x1024")
		finalQuality := firstNonEmpty(*quality, sceneQuality, cfg.Defaults.Quality, "high")

		var panelRefs []string
		for _, r := range panel.Refs {
			path := r
			if !filepath.IsAbs(path) {
				path = filepath.Join(configDir, r)
			}
			panelRefs = append(panelRefs, path)
		}
		allRefs := append(sceneRefs, panelRefs...)

		if !*force && generate.HasVersion(outputDir, panel.Page, finalQuality) {
			fmt.Fprintf(os.Stdout, "[%d/%d] page=%d scene=%s status=skip (%s exists)\n", idx, total, panel.Page, panel.Scene, finalQuality)
			skipped++
			continue
		}

		output := generate.NextVersion(outputDir, panel.Page, finalQuality)
		fullPrompt, err := generate.BuildPrompt(prompt, resolvedStyle, prefix)
		if err != nil {
			fmt.Fprintf(os.Stdout, "[%d/%d] page=%d scene=%s status=invalid (%v)\n", idx, total, panel.Page, panel.Scene, err)
			invalid++
			continue
		}

		fmt.Fprintf(os.Stdout, "[%d/%d] page=%d scene=%s status=plan\n", idx, total, panel.Page, panel.Scene)
		fmt.Fprintf(os.Stdout, "  output : %s\n", output)
		fmt.Fprintf(os.Stdout, "  size   : %s\n", finalSize)
		fmt.Fprintf(os.Stdout, "  quality: %s\n", finalQuality)
		fmt.Fprintf(os.Stdout, "  refs   : %d\n", len(allRefs))

		if *showRefs && len(allRefs) > 0 {
			for _, r := range allRefs {
				fmt.Fprintf(os.Stdout, "    - %s\n", r)
			}
		}

		if *showPrompt {
			fmt.Fprintln(os.Stdout, "  prompt:")
			for _, line := range strings.Split(fullPrompt, "\n") {
				fmt.Fprintf(os.Stdout, "    %s\n", line)
			}
		}

		planned++
	}

	fmt.Fprintf(os.Stdout, "\nPlan summary: %d planned, %d skipped, %d invalid (of %d)\n", planned, skipped, invalid, total)
	if invalid > 0 {
		os.Exit(1)
	}
}

func lintConfig(cfg *config.Config, configDir, styleFlag string, noStyle bool) []lintIssue {
	var issues []lintIssue
	add := func(level, msg string) {
		issues = append(issues, lintIssue{level: level, msg: msg})
	}

	if cfg.Defaults.Size != "" && !validSizes[cfg.Defaults.Size] {
		add("warning", fmt.Sprintf("defaults.size %q is non-standard", cfg.Defaults.Size))
	}
	if cfg.Defaults.Quality != "" && !validQualities[cfg.Defaults.Quality] {
		add("warning", fmt.Sprintf("defaults.quality %q is non-standard", cfg.Defaults.Quality))
	}

	if !noStyle {
		stylePath := styleFlag
		if stylePath == "" {
			stylePath = cfg.Style
			if stylePath == "" {
				stylePath = defaultStyle
			}
		}
		if !filepath.IsAbs(stylePath) {
			stylePath = filepath.Join(configDir, stylePath)
		}
		if _, err := os.Stat(stylePath); err != nil {
			add("warning", fmt.Sprintf("style file not found: %s", stylePath))
		}
	}

	for name, c := range cfg.Characters {
		for _, r := range c.Refs {
			path := r
			if !filepath.IsAbs(path) {
				path = filepath.Join(configDir, path)
			}
			if _, err := os.Stat(path); err != nil {
				add("warning", fmt.Sprintf("character %q ref not found: %s", name, path))
			}
		}
	}

	sceneNames := make([]string, 0, len(cfg.Scenes))
	for name := range cfg.Scenes {
		sceneNames = append(sceneNames, name)
	}
	sort.Strings(sceneNames)
	for _, name := range sceneNames {
		s := cfg.Scenes[name]
		if s.Size != "" && !validSizes[s.Size] {
			add("warning", fmt.Sprintf("scene %q size %q is non-standard", name, s.Size))
		}
		if s.Quality != "" && !validQualities[s.Quality] {
			add("warning", fmt.Sprintf("scene %q quality %q is non-standard", name, s.Quality))
		}
		for _, charName := range s.Characters {
			if _, ok := cfg.Characters[charName]; !ok {
				add("error", fmt.Sprintf("scene %q references unknown character %q", name, charName))
			}
		}
		for _, r := range s.Refs {
			path := r
			if !filepath.IsAbs(path) {
				path = filepath.Join(configDir, path)
			}
			if _, err := os.Stat(path); err != nil {
				add("warning", fmt.Sprintf("scene %q ref not found: %s", name, path))
			}
		}
	}

	if len(cfg.Panels) == 0 {
		add("error", "no panels defined")
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
		for _, r := range p.Refs {
			path := r
			if !filepath.IsAbs(path) {
				path = filepath.Join(configDir, path)
			}
			if _, err := os.Stat(path); err != nil {
				add("warning", fmt.Sprintf("panel[%d] ref not found: %s", i, path))
			}
		}
	}

	return issues
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
