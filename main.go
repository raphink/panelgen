// panelgen — AI image series generator.
//
// Usage:
//
//	panelgen generate {PROMPT | --prompt-file FILE} OUTPUT [options]
//	panelgen batch [options]
//	panelgen scenes [--config FILE]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/raphink/panelgen/internal/api"
	"github.com/raphink/panelgen/internal/config"
	"github.com/raphink/panelgen/internal/generate"
)

const defaultConfig = "panelgen.yml"
const defaultStyle = "style.txt"

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "generate", "gen":
		cmdGenerate(os.Args[2:])
	case "batch":
		cmdBatch(os.Args[2:])
	case "plan", "preview":
		cmdPlan(os.Args[2:])
	case "lint":
		cmdLint(os.Args[2:])
	case "scenes":
		cmdScenes(os.Args[2:])
	case "assemble":
		cmdAssemble(os.Args[2:])
	case "help", "--help", "-h":
		usage()
	case "version", "--version", "-V":
		fmt.Println("panelgen", version)
	default:
		fmt.Fprintf(os.Stderr, "panelgen: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `panelgen — AI image series generator

COMMANDS
  generate  Generate a single image
  batch     Generate all panels from a config file
  plan      Show what batch would generate without API calls
  lint      Validate config and local file references
  scenes    List scenes defined in a config file
  assemble  Assemble generated images into a PDF

Run 'panelgen COMMAND --help' for command-specific options.
`)
}

// ─── generate ─────────────────────────────────────────────────────────────────

func cmdGenerate(args []string) {
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: panelgen generate [PROMPT] OUTPUT [options]

Generate a single image. Provide the prompt inline or via --prompt-file.

ARGUMENTS
  PROMPT   Prompt text (omit if using --prompt-file)
  OUTPUT   Output PNG file path

OPTIONS
`)
		fs.PrintDefaults()
	}

	promptFile := fs.String("prompt-file", "", "Read prompt from `FILE` (for long prompts or agentic pipelines)")
	sceneName := fs.String("scene", "", "Use a named scene from the config file")
	configFile := fs.String("config", defaultConfig, "Config `FILE`")
	styleFile := fs.String("style", "", "Style guide text `FILE`")
	noStyle := fs.Bool("no-style", false, "Disable style guide")
	size := fs.String("size", "", "Image size (1024x1024 | 1024x1536 | 1536x1024)")
	quality := fs.String("quality", "", "Image quality (low | medium | high)")

	var refs []string
	fs.Func("ref", "Reference image `FILE` (repeatable)", func(v string) error {
		refs = append(refs, v)
		return nil
	})

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	// Positional args: [prompt] output
	positional := fs.Args()
	var prompt, output string
	switch len(positional) {
	case 1:
		output = positional[0]
	case 2:
		prompt = positional[0]
		output = positional[1]
	default:
		fs.Usage()
		os.Exit(1)
	}

	if prompt == "" && *promptFile == "" {
		fmt.Fprintln(os.Stderr, "panelgen: provide PROMPT or --prompt-file")
		fs.Usage()
		os.Exit(1)
	}
	if prompt != "" && *promptFile != "" {
		fmt.Fprintln(os.Stderr, "panelgen: PROMPT and --prompt-file are mutually exclusive")
		os.Exit(1)
	}
	if output == "" {
		fmt.Fprintln(os.Stderr, "panelgen: OUTPUT is required")
		fs.Usage()
		os.Exit(1)
	}

	// Load config (optional for generate, required for --scene)
	cfg := &config.Config{}
	configDir := "."
	if _, err := os.Stat(*configFile); err == nil {
		loaded, err := config.Load(*configFile)
		if err != nil {
			fatalf("load config: %v", err)
		}
		cfg = loaded
		configDir = filepath.Dir(*configFile)
	} else if *sceneName != "" {
		fatalf("--scene requires a config file (%s not found)", *configFile)
	}

	// Resolve scene
	scenePrefix := ""
	var sceneRefs []string
	sceneSize := ""
	sceneQuality := ""
	if *sceneName != "" {
		resolved, err := generate.ResolveScene(cfg, *sceneName, configDir)
		if err != nil {
			fatalf("%v", err)
		}
		scenePrefix = resolved.Prefix
		sceneRefs = resolved.Refs
		sceneSize = resolved.Size
		sceneQuality = resolved.Quality
	}

	finalSize := firstNonEmpty(*size, sceneSize, cfg.Defaults.Size, "1024x1024")
	finalQuality := firstNonEmpty(*quality, sceneQuality, cfg.Defaults.Quality, "high")
	allRefs := append(sceneRefs, refs...)

	// Resolve style file
	resolvedStyle := resolveStyle(*styleFile, *noStyle, cfg, configDir)

	// Validate refs exist
	for _, r := range allRefs {
		if _, err := os.Stat(r); err != nil {
			fatalf("reference image not found: %s", r)
		}
	}

	client, err := api.NewClientFromEnv()
	if err != nil {
		fatalf("%v", err)
	}

	if err := generate.Run(client, generate.Options{
		Prompt:      prompt,
		PromptFile:  *promptFile,
		Output:      output,
		StyleFile:   resolvedStyle,
		ScenePrefix: scenePrefix,
		Refs:        allRefs,
		Size:        finalSize,
		Quality:     finalQuality,
	}); err != nil {
		fatalf("%v", err)
	}
}

// ─── batch ────────────────────────────────────────────────────────────────────

func cmdBatch(args []string) {
	fs := flag.NewFlagSet("batch", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: panelgen batch [options]

Generate all panels defined in a config file.
Idempotent: skips panels that already have output at the requested quality.

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
	force := fs.Bool("force", false, "Generate new version even if output exists")
	dryRun := fs.Bool("dry-run", false, "Show what would be generated without calling the API")
	parallel := fs.Int("parallel", 1, "Number of parallel generations")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	cfgPath := *configFile
	if _, err := os.Stat(cfgPath); err != nil {
		fatalf("config file not found: %s", cfgPath)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fatalf("load config: %v", err)
	}
	configDir := filepath.Dir(cfgPath)

	resolvedStyle := resolveStyle(*styleFile, *noStyle, cfg, configDir)

	var pageList []int
	if *pages != "" {
		pageList, err = parsePageSpec(*pages)
		if err != nil {
			fatalf("parse --pages: %v", err)
		}
	}

	var client *api.Client
	if !*dryRun {
		client, err = api.NewClientFromEnv()
		if err != nil {
			fatalf("%v", err)
		}
	}

	if err := generate.Batch(client, generate.BatchOptions{
		Config:    cfg,
		ConfigDir: configDir,
		StyleFile: resolvedStyle,
		Pages:     pageList,
		Force:     *force,
		DryRun:    *dryRun,
		Size:      *size,
		Quality:   *quality,
		Parallel:  *parallel,
	}); err != nil {
		fatalf("%v", err)
	}
}

// ─── scenes ───────────────────────────────────────────────────────────────────

func cmdScenes(args []string) {
	fs := flag.NewFlagSet("scenes", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: panelgen scenes [--config FILE]\n\nOPTIONS\n")
		fs.PrintDefaults()
	}
	configFile := fs.String("config", defaultConfig, "Config `FILE`")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	cfg, err := config.Load(*configFile)
	if err != nil {
		fatalf("load config: %v", err)
	}

	if len(cfg.Scenes) == 0 {
		fmt.Fprintln(os.Stderr, "No scenes defined in config.")
		return
	}

	for name, scene := range cfg.Scenes {
		chars := strings.Join(scene.Characters, ", ")
		if chars == "" {
			chars = "—"
		}
		sz := scene.Size
		if sz == "" {
			sz = cfg.Defaults.Size
		}
		q := scene.Quality
		if q == "" {
			q = cfg.Defaults.Quality
		}
		fmt.Printf("  %s\n", name)
		if scene.Description != "" {
			fmt.Printf("    %s\n", scene.Description)
		}
		fmt.Printf("    characters : %s\n", chars)
		fmt.Printf("    size       : %s  quality: %s\n\n", sz, q)
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func resolveStyle(flagVal string, noStyle bool, cfg *config.Config, configDir string) string {
	if noStyle {
		return ""
	}
	if flagVal != "" {
		return flagVal
	}
	candidate := cfg.Style
	if candidate == "" {
		candidate = defaultStyle
	}
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(configDir, candidate)
	}
	if _, err := os.Stat(candidate); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: style file not found: %s, proceeding without style\n", candidate)
		return ""
	}
	return candidate
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func parsePageSpec(spec string) ([]int, error) {
	var pages []int
	seen := map[int]bool{}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			start, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid range %q", part)
			}
			end, err := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid range %q", part)
			}
			for i := start; i <= end; i++ {
				if !seen[i] {
					pages = append(pages, i)
					seen[i] = true
				}
			}
		} else {
			n, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid page number %q", part)
			}
			if !seen[n] {
				pages = append(pages, n)
				seen[n] = true
			}
		}
	}
	return pages, nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "panelgen: "+format+"\n", args...)
	os.Exit(1)
}
