// Package generate contains prompt building, versioning, and generation logic.
package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/raphink/panelgen/internal/api"
	"github.com/raphink/panelgen/internal/config"
)

// ─── Prompt building ──────────────────────────────────────────────────────────

func BuildPrompt(userPrompt string, styleFile string, scenePrefix string) (string, error) {
	var parts []string

	if styleFile != "" {
		data, err := os.ReadFile(styleFile)
		if err != nil {
			return "", fmt.Errorf("read style file: %w", err)
		}
		parts = append(parts, strings.TrimSpace(string(data)))
	}

	if scenePrefix != "" {
		parts = append(parts, strings.TrimSpace(scenePrefix))
	}

	parts = append(parts, userPrompt)
	return strings.Join(parts, "\n\n"), nil
}

// ─── Versioned output ─────────────────────────────────────────────────────────

func NextVersion(outputDir string, pageNum int, quality string) string {
	pattern := filepath.Join(outputDir, fmt.Sprintf("page_%d_%s-*.png", pageNum, quality))
	matches, _ := filepath.Glob(pattern)

	if len(matches) == 0 {
		return filepath.Join(outputDir, fmt.Sprintf("page_%d_%s-1.png", pageNum, quality))
	}

	re := regexp.MustCompile(`-(\d+)\.png$`)
	max := 0
	for _, m := range matches {
		if sub := re.FindStringSubmatch(m); sub != nil {
			if n, err := strconv.Atoi(sub[1]); err == nil && n > max {
				max = n
			}
		}
	}
	return filepath.Join(outputDir, fmt.Sprintf("page_%d_%s-%d.png", pageNum, quality, max+1))
}

func HasVersion(outputDir string, pageNum int, quality string) bool {
	pattern := filepath.Join(outputDir, fmt.Sprintf("page_%d_%s-*.png", pageNum, quality))
	matches, _ := filepath.Glob(pattern)
	return len(matches) > 0
}

// ─── Scene resolution ─────────────────────────────────────────────────────────

type ResolvedScene struct {
	Prefix  string
	Refs    []string
	Size    string
	Quality string
}

func ResolveScene(cfg *config.Config, sceneName string, configDir string) (*ResolvedScene, error) {
	scene, ok := cfg.Scenes[sceneName]
	if !ok {
		names := make([]string, 0, len(cfg.Scenes))
		for k := range cfg.Scenes {
			names = append(names, k)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("unknown scene %q — available: %s", sceneName, strings.Join(names, ", "))
	}

	var refs []string
	seen := map[string]bool{}

	addRef := func(r string) {
		path := r
		if !filepath.IsAbs(path) {
			path = filepath.Join(configDir, r)
		}
		if !seen[path] {
			seen[path] = true
			refs = append(refs, path)
		}
	}

	for _, charName := range scene.Characters {
		if char, ok := cfg.Characters[charName]; ok {
			for _, r := range char.Refs {
				addRef(r)
			}
		}
	}
	for _, r := range scene.Refs {
		addRef(r)
	}

	return &ResolvedScene{
		Prefix:  scene.PromptPrefix,
		Refs:    refs,
		Size:    scene.Size,
		Quality: scene.Quality,
	}, nil
}

// ─── Single image generation ──────────────────────────────────────────────────

type Options struct {
	Prompt      string
	PromptFile  string
	Output      string
	StyleFile   string
	ScenePrefix string
	Refs        []string
	Size        string
	Quality     string
}

func Run(client *api.Client, opts Options) error {
	prompt := opts.Prompt
	if opts.PromptFile != "" {
		data, err := os.ReadFile(opts.PromptFile)
		if err != nil {
			return fmt.Errorf("read prompt file: %w", err)
		}
		prompt = strings.TrimSpace(string(data))
	}

	fullPrompt, err := BuildPrompt(prompt, opts.StyleFile, opts.ScenePrefix)
	if err != nil {
		return err
	}

	var imgData []byte
	if len(opts.Refs) > 0 {
		fmt.Fprintf(os.Stderr, "Editing with %d reference(s): %s (%s, %s)\n",
			len(opts.Refs), opts.Output, opts.Size, opts.Quality)
		imgData, err = client.Edit(fullPrompt, opts.Refs, opts.Size, opts.Quality)
	} else {
		fmt.Fprintf(os.Stderr, "Generating: %s (%s, %s)\n", opts.Output, opts.Size, opts.Quality)
		imgData, err = client.Generate(fullPrompt, opts.Size, opts.Quality)
	}
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(opts.Output), 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	if err := os.WriteFile(opts.Output, imgData, 0644); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Saved: %s\n", opts.Output)
	return nil
}

// ─── Batch generation ─────────────────────────────────────────────────────────

type BatchOptions struct {
	Config    *config.Config
	ConfigDir string
	StyleFile string
	Pages     []int // nil = all
	Force     bool
	DryRun    bool
	Size      string // overrides config/scene
	Quality   string // overrides config/scene
	Parallel  int
}

type workItem struct {
	index   int
	total   int
	pageNum int
	scene   string
	prompt  string
	output  string
	size    string
	quality string
	refs    []string
	prefix  string
}

func Batch(client *api.Client, opts BatchOptions) error {
	cfg := opts.Config
	panels := cfg.Panels
	if len(panels) == 0 {
		return fmt.Errorf("no panels defined in config")
	}

	// Filter by page list
	if len(opts.Pages) > 0 {
		pageSet := make(map[int]bool, len(opts.Pages))
		for _, p := range opts.Pages {
			pageSet[p] = true
		}
		filtered := panels[:0]
		for _, p := range panels {
			if pageSet[p.Page] {
				filtered = append(filtered, p)
			}
		}
		panels = filtered
		if len(panels) == 0 {
			return fmt.Errorf("no panels match the requested page list")
		}
	}

	outputDir := filepath.Join(opts.ConfigDir, cfg.OutputDir)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	total := len(panels)
	skipped := 0
	var work []workItem

	for i, panel := range panels {
		idx := i + 1
		prompt := strings.TrimSpace(panel.Prompt)
		if prompt == "" || panel.Scene == "blank" {
			fmt.Fprintf(os.Stderr, "[%d/%d] Page %d: skipping (blank)\n", idx, total, panel.Page)
			skipped++
			continue
		}

		// Resolve scene
		prefix := ""
		var sceneRefs []string
		sceneSize := ""
		sceneQuality := ""

		if panel.Scene != "" {
			resolved, err := ResolveScene(cfg, panel.Scene, opts.ConfigDir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[%d/%d] Page %d: %v — skipping\n", idx, total, panel.Page, err)
				skipped++
				continue
			}
			prefix = resolved.Prefix
			sceneRefs = resolved.Refs
			sceneSize = resolved.Size
			sceneQuality = resolved.Quality
		}

		size := opts.Size
		if size == "" {
			size = sceneSize
		}
		if size == "" {
			size = cfg.Defaults.Size
		}

		quality := opts.Quality
		if quality == "" {
			quality = sceneQuality
		}
		if quality == "" {
			quality = cfg.Defaults.Quality
		}

		// Panel-level refs appended after scene refs
		var panelRefs []string
		for _, r := range panel.Refs {
			path := r
			if !filepath.IsAbs(path) {
				path = filepath.Join(opts.ConfigDir, r)
			}
			panelRefs = append(panelRefs, path)
		}
		allRefs := append(sceneRefs, panelRefs...)

		// Idempotency
		if HasVersion(outputDir, panel.Page, quality) && !opts.Force {
			fmt.Fprintf(os.Stderr, "[%d/%d] Page %d: %s version exists, skipping (--force for new increment)\n",
				idx, total, panel.Page, quality)
			skipped++
			continue
		}

		output := NextVersion(outputDir, panel.Page, quality)

		if opts.DryRun {
			fmt.Fprintf(os.Stderr, "[%d/%d] Page %d (%s): would generate %s (%s, %s)\n",
				idx, total, panel.Page, panel.Scene, filepath.Base(output), size, quality)
			continue
		}

		work = append(work, workItem{
			index:   idx,
			total:   total,
			pageNum: panel.Page,
			scene:   panel.Scene,
			prompt:  prompt,
			output:  output,
			size:    size,
			quality: quality,
			refs:    allRefs,
			prefix:  prefix,
		})
	}

	if opts.DryRun {
		fmt.Fprintf(os.Stderr, "\nDry run: %d would be generated, %d skipped (of %d)\n",
			len(work), skipped, total)
		return nil
	}
	if len(work) == 0 {
		fmt.Fprintf(os.Stderr, "\nDone: 0 generated, %d skipped, 0 failed (of %d)\n", skipped, total)
		return nil
	}

	parallel := opts.Parallel
	if parallel < 1 {
		parallel = 1
	}

	generated := 0
	failed := 0
	var mu sync.Mutex

	runOne := func(item workItem) {
		fmt.Fprintf(os.Stderr, "[%d/%d] Page %d (%s): generating...\n",
			item.index, item.total, item.pageNum, item.scene)

		fullPrompt, err := BuildPrompt(item.prompt, opts.StyleFile, item.prefix)
		if err != nil {
			mu.Lock()
			fmt.Fprintf(os.Stderr, "  Page %d FAILED: %v\n", item.pageNum, err)
			failed++
			mu.Unlock()
			return
		}

		var imgData []byte
		if len(item.refs) > 0 {
			imgData, err = client.Edit(fullPrompt, item.refs, item.size, item.quality)
		} else {
			imgData, err = client.Generate(fullPrompt, item.size, item.quality)
		}

		if err != nil {
			mu.Lock()
			fmt.Fprintf(os.Stderr, "  Page %d FAILED: %v\n", item.pageNum, err)
			failed++
			mu.Unlock()
			return
		}

		if werr := os.WriteFile(item.output, imgData, 0644); werr != nil {
			mu.Lock()
			fmt.Fprintf(os.Stderr, "  Page %d FAILED (write): %v\n", item.pageNum, werr)
			failed++
			mu.Unlock()
			return
		}

		mu.Lock()
		fmt.Fprintf(os.Stderr, "  Saved: %s\n", item.output)
		generated++
		mu.Unlock()
	}

	if parallel == 1 {
		for _, item := range work {
			runOne(item)
		}
	} else {
		sem := make(chan struct{}, parallel)
		var wg sync.WaitGroup
		for _, item := range work {
			item := item
			sem <- struct{}{}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				runOne(item)
			}()
		}
		wg.Wait()
	}

	fmt.Fprintf(os.Stderr, "\nDone: %d generated, %d skipped, %d failed (of %d panels)\n",
		generated, skipped, failed, total)

	if failed > 0 {
		return fmt.Errorf("%d panel(s) failed", failed)
	}
	return nil
}
