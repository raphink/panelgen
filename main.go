// panelgen — AI image series generator.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/raphink/panelgen/internal/api"
	"github.com/raphink/panelgen/internal/config"
	"github.com/raphink/panelgen/internal/generate"
	"github.com/raphink/panelgen/internal/ui"
)

const defaultConfig = "panelgen.yml"

var version = "dev"

// Persistent flags shared by all subcommands.
var (
	configFile string
	styleFile  string
	noStyle    bool
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// ─── Root ─────────────────────────────────────────────────────────────────────

var rootCmd = &cobra.Command{
	Use:     "panelgen",
	Short:   "AI image series generator",
	Version: version,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configFile, "config", defaultConfig, "Config `FILE`")
	rootCmd.PersistentFlags().StringVar(&styleFile, "style", "", "Style guide text `FILE`")
	rootCmd.PersistentFlags().BoolVar(&noStyle, "no-style", false, "Disable style guide")

	rootCmd.AddCommand(generateCmd)
	rootCmd.AddCommand(batchCmd)
	rootCmd.AddCommand(planCmd)
	rootCmd.AddCommand(lintCmd)
	rootCmd.AddCommand(scenesCmd)
	rootCmd.AddCommand(assembleCmd)
	rootCmd.AddCommand(charactersCmd)
	charactersCmd.AddCommand(charactersListCmd)
	charactersCmd.AddCommand(charactersGenerateCmd)
}

// ─── generate ─────────────────────────────────────────────────────────────────

var (
	genPromptFile string
	genScene      string
	genSize       string
	genQuality    string
	genRefs       []string
)

var generateCmd = &cobra.Command{
	Use:     "generate [PROMPT] OUTPUT",
	Aliases: []string{"gen"},
	Short:   "Generate a single image",
	Long: `Generate a single image. Provide the prompt inline or via --prompt-file.

ARGUMENTS
  PROMPT   Prompt text (omit if using --prompt-file)
  OUTPUT   Output PNG file path`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		var prompt, output string
		switch len(args) {
		case 1:
			output = args[0]
		case 2:
			prompt = args[0]
			output = args[1]
		}
		if prompt == "" && genPromptFile == "" {
			return fmt.Errorf("provide PROMPT or --prompt-file")
		}
		if prompt != "" && genPromptFile != "" {
			return fmt.Errorf("PROMPT and --prompt-file are mutually exclusive")
		}

		cfg, configDir := loadOptionalConfig(configFile, genScene)

		scenePrefix := ""
		var sceneRefs []string
		sceneSize, sceneQuality := "", ""
		if genScene != "" {
			resolved, err := generate.ResolveScene(cfg, genScene, configDir, nil)
			if err != nil {
				fatalf("%v", err)
			}
			scenePrefix = resolved.Prefix
			sceneRefs = resolved.Refs
			sceneSize = resolved.Size
			sceneQuality = resolved.Quality
		}

		finalSize := firstNonEmpty(genSize, sceneSize, cfg.Defaults.Size, "1024x1024")
		finalQuality := firstNonEmpty(genQuality, sceneQuality, cfg.Defaults.Quality, "high")
		if !isValidSize(finalSize) {
			fatalf("invalid size %q (must be WxH with both dimensions divisible by 16 and ≤8,294,400 total pixels)", finalSize)
		}
		if !validQualities[finalQuality] {
			fatalf("invalid quality %q (expected one of: low, medium, high)", finalQuality)
		}
		allRefs := append(sceneRefs, genRefs...)
		resolvedStyle := resolveStyle(styleFile, noStyle, cfg, configDir)

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
			PromptFile:  genPromptFile,
			Output:      output,
			StyleFile:   resolvedStyle,
			ScenePrefix: scenePrefix,
			Refs:        allRefs,
			Size:        finalSize,
			Quality:     finalQuality,
		}); err != nil {
			fatalf("%v", err)
		}
		return nil
	},
}

func init() {
	generateCmd.Flags().StringVar(&genPromptFile, "prompt-file", "", "Read prompt from `FILE` (for long prompts or agentic pipelines)")
	generateCmd.Flags().StringVar(&genScene, "scene", "", "Use a named scene from the config file")
	generateCmd.Flags().StringVar(&genSize, "size", "", "Image size as WxH (both dims divisible by 16, ≤8,294,400 total px)")
	generateCmd.Flags().StringVar(&genQuality, "quality", "", "Image quality (low | medium | high)")
	generateCmd.Flags().StringArrayVar(&genRefs, "ref", nil, "Reference image `FILE` (repeatable)")
}

// ─── batch ────────────────────────────────────────────────────────────────────

var (
	batchSize      string
	batchQuality   string
	batchPages     string
	batchForce     bool
	batchDryRun    bool
	batchParallel  int
	batchAssemble  bool
	batchOutputDir string
)

var batchCmd = &cobra.Command{
	Use:   "batch",
	Short: "Generate all panels from a config file",
	Long: `Generate all panels defined in a config file.
Idempotent: skips panels that already have output at the requested quality.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := os.Stat(configFile); err != nil {
			fatalf("config file not found: %s", configFile)
		}

		cfg, err := config.Load(configFile)
		if err != nil {
			fatalf("load config: %v", err)
		}
		configDir := filepath.Dir(configFile)
		resolvedStyle := resolveStyle(styleFile, noStyle, cfg, configDir)
		requireNoPreflightErrors(generationPreflightIssues(cfg, configDir, styleFile, noStyle, batchSize, batchQuality))

		var pageList []int
		if batchPages != "" {
			pageList, err = parsePageSpec(batchPages)
			if err != nil {
				fatalf("parse --pages: %v", err)
			}
		}

		var client *api.Client
		if !batchDryRun {
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
			Force:     batchForce,
			DryRun:    batchDryRun,
			Size:      batchSize,
			Quality:   batchQuality,
			Parallel:  batchParallel,
			OutputDir: batchOutputDir,
		}); err != nil {
			fatalf("%v", err)
		}

		if batchAssemble || (cfg.Defaults.Assemble != nil && *cfg.Defaults.Assemble) {
			runAssemble(configFile, batchOutputDir, "", false, false)
		}
		return nil
	},
}

func init() {
	batchCmd.Flags().StringVar(&batchSize, "size", "", "Override image size for all panels")
	batchCmd.Flags().StringVar(&batchQuality, "quality", "", "Override image quality for all panels")
	batchCmd.Flags().StringVar(&batchPages, "pages", "", "Page subset, e.g. '1,3,5-10,20'")
	batchCmd.Flags().BoolVar(&batchForce, "force", false, "Generate new version even if output exists")
	batchCmd.Flags().BoolVar(&batchDryRun, "dry-run", false, "Show what would be generated without calling the API")
	batchCmd.Flags().IntVar(&batchParallel, "parallel", 1, "Number of parallel generations")
	batchCmd.Flags().BoolVar(&batchAssemble, "assemble", false, "Assemble PDF after generation (overrides defaults.assemble)")
	batchCmd.Flags().StringVar(&batchOutputDir, "output-dir", "", "Override output directory (default: output_dir from config)")
}

// ─── plan ─────────────────────────────────────────────────────────────────────

var (
	planSize       string
	planQuality    string
	planPages      string
	planForce      bool
	planShowPrompt bool
	planShowRefs   bool
)

var planCmd = &cobra.Command{
	Use:     "plan",
	Aliases: []string{"preview"},
	Short:   "Show what batch would generate without API calls",
	Long: `Preview batch generation: resolved outputs, refs, and prompts (optional).
No API calls are made.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, configDir := mustLoadConfig(configFile)
		resolvedStyle := resolveStyle(styleFile, noStyle, cfg, configDir)
		requireNoPreflightErrors(generationPreflightIssues(cfg, configDir, styleFile, noStyle, planSize, planQuality))

		panels := filterPanelsByPage(cfg.Panels, planPages)
		if len(panels) == 0 {
			fatalf("no panels to plan")
		}

		outputDir := filepath.Join(configDir, cfg.OutputDir)
		_ = os.MkdirAll(outputDir, 0755)

		total := len(panels)
		planned, skipped, invalid := 0, 0, 0
		for i, panel := range panels {
			result := planOnePanel(panel, cfg, configDir, outputDir, planSize, planQuality, resolvedStyle, planForce)
			planned, skipped, invalid = printPlanResult(result, panel, i+1, total, planShowRefs, planShowPrompt, planned, skipped, invalid)
		}

		fmt.Fprintf(os.Stdout, "\n%s %s%s%s%s%s%s\n",
			ui.IconPlan,
			ui.BoldCyan(fmt.Sprintf("%d planned", planned)), ui.Sep(),
			ui.Yellow(fmt.Sprintf("%d skipped", skipped)), ui.Sep(),
			ui.BoldRed(fmt.Sprintf("%d invalid", invalid)),
			ui.Dim(fmt.Sprintf(" (of %d)", total)))
		if invalid > 0 {
			os.Exit(1)
		}
		return nil
	},
}

func init() {
	planCmd.Flags().StringVar(&planSize, "size", "", "Override image size for all panels")
	planCmd.Flags().StringVar(&planQuality, "quality", "", "Override image quality for all panels")
	planCmd.Flags().StringVar(&planPages, "pages", "", "Page subset, e.g. '1,3,5-10,20'")
	planCmd.Flags().BoolVar(&planForce, "force", false, "Show a new version even if output exists")
	planCmd.Flags().BoolVar(&planShowPrompt, "show-prompt", false, "Show full resolved prompt per panel")
	planCmd.Flags().BoolVar(&planShowRefs, "show-refs", false, "List all resolved refs per panel")
}

// ─── lint ─────────────────────────────────────────────────────────────────────

var lintStrict bool

var lintCmd = &cobra.Command{
	Use:   "lint",
	Short: "Validate config and local file references",
	Long:  `Validate config shape, scene/character references, and local file paths.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, configDir, loadWarnings := mustLoadConfigWithWarnings(configFile)
		issues := append(loadWarningsAsIssues(loadWarnings), lintConfig(cfg, configDir, styleFile, noStyle)...)

		errors, warnings := 0, 0
		for _, i := range issues {
			if i.level == "error" {
				fmt.Fprintf(os.Stderr, "%s %s\n", ui.IconFail, ui.Red(i.msg))
				errors++
			} else {
				fmt.Fprintf(os.Stderr, "%s %s\n", ui.IconWarn, ui.Yellow(i.msg))
				warnings++
			}
		}

		if errors == 0 && warnings == 0 {
			fmt.Fprintf(os.Stderr, "%s No issues found\n", ui.IconOK)
		} else {
			fmt.Fprintf(os.Stderr, "\n%s%s%s\n",
				ui.BoldRed(fmt.Sprintf("%d error(s)", errors)), ui.Sep(),
				ui.BoldYellow(fmt.Sprintf("%d warning(s)", warnings)))
		}
		if errors > 0 || (lintStrict && warnings > 0) {
			os.Exit(1)
		}
		return nil
	},
}

func init() {
	lintCmd.Flags().BoolVar(&lintStrict, "strict", false, "Treat warnings as errors")
}

// ─── scenes ───────────────────────────────────────────────────────────────────

var scenesCmd = &cobra.Command{
	Use:   "scenes",
	Short: "List scenes defined in a config file",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(configFile)
		if err != nil {
			fatalf("load config: %v", err)
		}

		if len(cfg.Scenes) == 0 {
			fmt.Fprintln(os.Stderr, "No scenes defined in config.")
			return nil
		}

		for name, scene := range cfg.Scenes {
			chars := strings.Join(scene.Characters, ", ")
			if chars == "" {
				chars = ui.Dim("—")
			}
			sz := firstNonEmpty(scene.Size, cfg.Defaults.Size)
			q := firstNonEmpty(scene.Quality, cfg.Defaults.Quality)
			fmt.Printf("%s %s\n", ui.IconScene, ui.Bold(name))
			if scene.Description != "" {
				fmt.Printf("  %s %s\n", ui.Dim("desc      "), scene.Description)
			}
			fmt.Printf("  %s %s\n", ui.Dim("characters"), chars)
			fmt.Printf("  %s %s%s%s\n\n", ui.Dim("size      "), sz, ui.Sep(), q)
		}
		return nil
	},
}

// ─── characters ───────────────────────────────────────────────────────────────

var charactersCmd = &cobra.Command{
	Use:     "characters",
	Aliases: []string{"chars"},
	Short:   "Manage characters",
	Long: `Manage characters defined in the config.

COMMANDS
  list      List characters and their prompts
  generate  Generate reference images for characters

Run 'panelgen characters <command> --help' for options.`,
}

var charactersListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List characters and their prompts",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := mustLoadConfig(configFile)
		for _, name := range sortedCharacterNames(cfg) {
			char := cfg.Characters[name]
			refs := ui.Dim(fmt.Sprintf("(%d ref(s))", len(char.Refs)))
			fmt.Printf("%s %-22s %s %s\n", ui.IconChar, ui.Bold(name), refs, char.Prompt)
		}
		return nil
	},
}

const defaultCharacterPreprompt = "Full body portrait on a plain solid-color background. Character reference sheet."

var (
	charsGenOutputDir  string
	charsGenSize       string
	charsGenQuality    string
	charsGenAll        bool
	charsGenShowPrompt bool
	charsGenPreprompt  string
)

var charactersGenerateCmd = &cobra.Command{
	Use:     "generate [NAME...]",
	Aliases: []string{"gen"},
	Short:   "Generate reference images for characters",
	Long: `Generate reference images for characters defined in the config.
Output: <characters_dir>/<name>-<N>.png`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, configDir := mustLoadConfig(configFile)

		names := args
		if charsGenAll {
			names = sortedCharacterNames(cfg)
		}
		if len(names) == 0 {
			return fmt.Errorf("specify character name(s), or use --all")
		}
		for _, name := range names {
			if _, ok := cfg.Characters[name]; !ok {
				fatalf("unknown character %q", name)
			}
		}

		resolvedOutput := charsGenOutputDir
		if resolvedOutput == "" && cfg.Defaults.CharactersDir != "" {
			resolvedOutput = filepath.Join(configDir, cfg.Defaults.CharactersDir)
		}
		if resolvedOutput == "" {
			resolvedOutput = filepath.Join(configDir, "characters")
		}

		resolvedStyle := resolveStyle(styleFile, noStyle, cfg, configDir)
		finalSize := firstNonEmpty(charsGenSize, cfg.Defaults.Size, "1024x1024")
		finalQuality := firstNonEmpty(charsGenQuality, cfg.Defaults.Quality, "high")
		requireNoPreflightErrors(lintSizeQualityIssues("characters", finalSize, finalQuality))
		preprompt := firstNonEmpty(charsGenPreprompt, cfg.Defaults.CharactersPreprompt, defaultCharacterPreprompt)

		var client *api.Client
		if !charsGenShowPrompt {
			if err := os.MkdirAll(resolvedOutput, 0755); err != nil {
				fatalf("create output dir: %v", err)
			}
			var err error
			client, err = api.NewClientFromEnv()
			if err != nil {
				fatalf("%v", err)
			}
		}

		failed := 0
		for _, name := range names {
			char := cfg.Characters[name]
			if char.Prompt == "" {
				fmt.Fprintf(os.Stderr, "%s %s%sno prompt\n", ui.IconSkip, ui.Bold(name), ui.Sep())
				continue
			}

			// Always use the YAML refs when generating a character image —
			// the point is to regenerate from the original concept.
			var refs []string
			for _, r := range char.Refs {
				if filepath.IsAbs(r) {
					refs = append(refs, r)
				} else {
					refs = append(refs, filepath.Join(configDir, r))
				}
			}

			if charsGenShowPrompt {
				prompt, err := generate.BuildPrompt(char.Prompt, resolvedStyle, preprompt)
				if err != nil {
					fmt.Fprintf(os.Stderr, "%s %s%sbuild prompt: %v\n", ui.IconFail, ui.Bold(name), ui.Sep(), err)
					failed++
					continue
				}
				fmt.Printf("%s %s%s%s%s%s\n",
					ui.IconChar, ui.Bold(name), ui.Sep(),
					finalSize, ui.Sep(), finalQuality)
				if len(refs) == 0 {
					fmt.Printf("  %s %s\n", ui.Dim("refs  "), ui.Dim("(none)"))
				} else {
					fmt.Printf("  %s\n", ui.Dim("refs  "))
					for _, r := range refs {
						fmt.Printf("    %s %s\n", ui.Dim("·"), r)
					}
				}
				fmt.Printf("  %s\n", ui.Dim("prompt"))
				for _, line := range strings.Split(prompt, "\n") {
					fmt.Printf("    %s\n", ui.Dim(line))
				}
				fmt.Println()
				continue
			}

			output := nextCharacterVersion(resolvedOutput, name)
			fmt.Fprintf(os.Stderr, "%s Generating%s%s%s%s%s%s\n",
				ui.IconChar, ui.Sep(), ui.Bold(name), ui.Sep(),
				finalSize, ui.Sep(), finalQuality)
			if err := generate.Run(client, generate.Options{
				Prompt:      char.Prompt,
				StyleFile:   resolvedStyle,
				ScenePrefix: preprompt,
				Refs:        refs,
				Output:      output,
				Size:        finalSize,
				Quality:     finalQuality,
			}); err != nil {
				fmt.Fprintf(os.Stderr, "  %s %s%s%v\n", ui.IconFail, ui.Bold(name), ui.Sep(), err)
				failed++
			}
		}

		if failed > 0 {
			fatalf("%d character(s) failed", failed)
		}
		return nil
	},
}

func init() {
	charactersGenerateCmd.Flags().StringVar(&charsGenOutputDir, "output-dir", "", "Directory for character images (default: characters_dir from config, or 'characters/')")
	charactersGenerateCmd.Flags().StringVar(&charsGenSize, "size", "", "Image size (overrides defaults)")
	charactersGenerateCmd.Flags().StringVar(&charsGenQuality, "quality", "", "Image quality (overrides defaults)")
	charactersGenerateCmd.Flags().BoolVar(&charsGenAll, "all", false, "Generate all characters")
	charactersGenerateCmd.Flags().BoolVar(&charsGenShowPrompt, "show-prompt", false, "Print resolved prompt and refs without calling the API")
	charactersGenerateCmd.Flags().StringVar(&charsGenPreprompt, "preprompt", "", "Preprompt prefix for character generation (default: solid-background reference sheet instruction)")
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func loadOptionalConfig(cfgFile, sceneName string) (*config.Config, string) {
	cfg := &config.Config{}
	configDir := "."
	if _, err := os.Stat(cfgFile); err == nil {
		loaded, err := config.Load(cfgFile)
		if err != nil {
			fatalf("load config: %v", err)
		}
		cfg = loaded
		configDir = filepath.Dir(cfgFile)
	} else if sceneName != "" {
		fatalf("--scene requires a config file (%s not found)", cfgFile)
	}
	return cfg, configDir
}

func resolveStyle(flagVal string, noStyleFlag bool, cfg *config.Config, configDir string) string {
	if noStyleFlag {
		return ""
	}
	if flagVal != "" {
		return flagVal
	}
	candidate := cfg.Style
	if candidate == "" {
		return ""
	}
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(configDir, candidate)
	}
	if _, err := os.Stat(candidate); err != nil {
		fmt.Fprintf(os.Stderr, "%s style file not found: %s%sproceeding without style\n", ui.IconWarn, candidate, ui.Sep())
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
			if start > end {
				return nil, fmt.Errorf("invalid range %q: start must be <= end", part)
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
