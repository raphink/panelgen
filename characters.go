package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"github.com/raphink/panelgen/internal/api"
	"github.com/raphink/panelgen/internal/config"
	"github.com/raphink/panelgen/internal/generate"
)

func cmdCharacters(args []string) {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, `Usage: panelgen characters <command>

COMMANDS
  list      List characters and their prompts
  generate  Generate reference images for characters

Run 'panelgen characters <command> --help' for options.
`)
		os.Exit(1)
	}

	switch args[0] {
	case "list", "ls":
		cmdCharactersList(args[1:])
	case "generate", "gen":
		cmdCharactersGenerate(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "panelgen characters: unknown command %q\n", args[0])
		os.Exit(1)
	}
}

func cmdCharactersList(args []string) {
	fs := flag.NewFlagSet("characters list", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: panelgen characters list [options]

List characters defined in the config and their prompts.

OPTIONS
`)
		fs.PrintDefaults()
	}
	configFile := fs.String("config", defaultConfig, "Config `FILE`")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	cfg, _ := mustLoadConfig(*configFile)
	for _, name := range sortedCharacterNames(cfg) {
		char := cfg.Characters[name]
		fmt.Printf("%-20s %s\n", name, char.Prompt)
	}
}

func cmdCharactersGenerate(args []string) {
	fs := flag.NewFlagSet("characters generate", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: panelgen characters generate [options] [NAME...]

Generate reference images for characters defined in the config.
Output: <characters_dir>/<name>-<N>.png

OPTIONS
`)
		fs.PrintDefaults()
	}

	configFile := fs.String("config", defaultConfig, "Config `FILE`")
	styleFile := fs.String("style", "", "Style guide text `FILE`")
	noStyle := fs.Bool("no-style", false, "Disable style guide")
	outputDir := fs.String("output-dir", "", "Directory for character images (default: characters_dir from config, or 'characters/')")
	size := fs.String("size", "", "Image size (overrides defaults)")
	quality := fs.String("quality", "", "Image quality (overrides defaults)")
	all := fs.Bool("all", false, "Generate all characters")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	cfg, configDir := mustLoadConfig(*configFile)

	names := fs.Args()
	if *all {
		names = sortedCharacterNames(cfg)
	}
	if len(names) == 0 {
		fmt.Fprintln(os.Stderr, "panelgen: specify character name(s), or use --all")
		fs.Usage()
		os.Exit(1)
	}

	for _, name := range names {
		if _, ok := cfg.Characters[name]; !ok {
			fatalf("unknown character %q", name)
		}
	}

	resolvedOutput := *outputDir
	if resolvedOutput == "" && cfg.Defaults.CharactersDir != "" {
		resolvedOutput = filepath.Join(configDir, cfg.Defaults.CharactersDir)
	}
	if resolvedOutput == "" {
		resolvedOutput = filepath.Join(configDir, "characters")
	}
	if err := os.MkdirAll(resolvedOutput, 0755); err != nil {
		fatalf("create output dir: %v", err)
	}

	resolvedStyle := resolveStyle(*styleFile, *noStyle, cfg, configDir)
	finalSize := firstNonEmpty(*size, cfg.Defaults.Size, "1024x1024")
	finalQuality := firstNonEmpty(*quality, cfg.Defaults.Quality, "high")

	client, err := api.NewClientFromEnv()
	if err != nil {
		fatalf("%v", err)
	}

	failed := 0
	for _, name := range names {
		char := cfg.Characters[name]
		if char.Prompt == "" {
			fmt.Fprintf(os.Stderr, "  %s: skipping (no prompt)\n", name)
			continue
		}
		output := nextCharacterVersion(resolvedOutput, name)
		if err := generate.Run(client, generate.Options{
			Prompt:    char.Prompt,
			StyleFile: resolvedStyle,
			Output:    output,
			Size:      finalSize,
			Quality:   finalQuality,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "  %s FAILED: %v\n", name, err)
			failed++
		}
	}

	if failed > 0 {
		fatalf("%d character(s) failed", failed)
	}
}

func sortedCharacterNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Characters))
	for name := range cfg.Characters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

var charVersionRe = regexp.MustCompile(`-(\d+)\.png$`)

func nextCharacterVersion(dir, name string) string {
	pattern := filepath.Join(dir, name+"-*.png")
	matches, _ := filepath.Glob(pattern)
	max := 0
	for _, m := range matches {
		if sub := charVersionRe.FindStringSubmatch(m); sub != nil {
			if n, err := strconv.Atoi(sub[1]); err == nil && n > max {
				max = n
			}
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%d.png", name, max+1))
}
