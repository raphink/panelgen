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
	"time"

	"github.com/raphink/panelgen/internal/api"
	"github.com/raphink/panelgen/internal/config"
	"github.com/raphink/panelgen/internal/ui"
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

// BestPageImage returns the path of the best existing image for a page:
// highest quality (high > medium > low) then highest increment.
// Returns "" if no image exists for that page.
func BestPageImage(outputDir string, pageNum int) string {
	qualityRank := map[string]int{"high": 3, "medium": 2, "low": 1}
	re := regexp.MustCompile(`^page_\d+_(low|medium|high)-(\d+)\.png$`)
	best, bestQ, bestN := "", 0, 0
	for _, q := range []string{"high", "medium", "low"} {
		pattern := filepath.Join(outputDir, fmt.Sprintf("page_%d_%s-*.png", pageNum, q))
		matches, _ := filepath.Glob(pattern)
		for _, m := range matches {
			sub := re.FindStringSubmatch(filepath.Base(m))
			if sub == nil {
				continue
			}
			n, _ := strconv.Atoi(sub[2])
			if qualityRank[q] > bestQ || (qualityRank[q] == bestQ && n > bestN) {
				best, bestQ, bestN = m, qualityRank[q], n
			}
		}
	}
	return best
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

// ResolveCharacters returns prompt snippets (prefixed with the character name) and
// absolute ref paths for a list of character names, for injection into scene/panel prompts.
// If a generated character image exists in the characters dir, it is used as the ref
// instead of the YAML refs — so panels automatically pick up the latest generated version.
func ResolveCharacters(cfg *config.Config, names []string, configDir string) (prompts, refs []string) {
	charsDir := cfg.Defaults.CharactersDir
	if charsDir == "" {
		charsDir = "characters"
	}
	if !filepath.IsAbs(charsDir) {
		charsDir = filepath.Join(configDir, charsDir)
	}

	seen := map[string]bool{}
	addRef := func(path string) {
		if !seen[path] {
			seen[path] = true
			refs = append(refs, path)
		}
	}

	for _, name := range names {
		char, ok := cfg.Characters[name]
		if !ok {
			continue
		}
		if char.Prompt != "" {
			prompts = append(prompts, fmt.Sprintf("Character %q: %s", name, strings.TrimSpace(char.Prompt)))
		}
		if latest := latestCharacterRef(charsDir, name); latest != "" {
			addRef(latest)
		} else {
			for _, r := range char.Refs {
				path := r
				if !filepath.IsAbs(path) {
					path = filepath.Join(configDir, r)
				}
				addRef(path)
			}
		}
	}
	return
}

var charVersionRe = regexp.MustCompile(`-(\d+)\.png$`)

// latestCharacterRef returns the highest-numbered <name>-N.png in dir, or "" if none exist.
func latestCharacterRef(dir, name string) string {
	matches, _ := filepath.Glob(filepath.Join(dir, name+"-*.png"))
	best, max := "", 0
	for _, m := range matches {
		if sub := charVersionRe.FindStringSubmatch(m); sub != nil {
			if n, err := strconv.Atoi(sub[1]); err == nil && n > max {
				max, best = n, m
			}
		}
	}
	return best
}

func ResolveScene(cfg *config.Config, sceneName string, configDir string, panelVars map[string]string) (*ResolvedScene, error) {
	scene, ok := cfg.Scenes[sceneName]
	if !ok {
		names := make([]string, 0, len(cfg.Scenes))
		for k := range cfg.Scenes {
			names = append(names, k)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("unknown scene %q — available: %s", sceneName, strings.Join(names, ", "))
	}

	charDescriptions, charRefs := ResolveCharacters(cfg, scene.Characters, configDir)

	seen := map[string]bool{}
	addRef := func(r string) {
		path := r
		if !filepath.IsAbs(path) {
			path = filepath.Join(configDir, r)
		}
		if !seen[path] {
			seen[path] = true
			charRefs = append(charRefs, path)
		}
	}
	for _, r := range charRefs {
		seen[r] = true
	}
	for _, r := range scene.Refs {
		addRef(r)
	}

	prefix := strings.Join(charDescriptions, "\n\n")
	if scene.PromptPrefix != "" {
		if prefix != "" {
			prefix += "\n\n"
		}
		prefix += applyVars(strings.TrimSpace(scene.PromptPrefix), scene.Vars, panelVars)
	}

	return &ResolvedScene{
		Prefix:  prefix,
		Refs:    charRefs,
		Size:    scene.Size,
		Quality: scene.Quality,
	}, nil
}

// applyVars substitutes {key} placeholders using scene defaults overridden by panel vars.
func applyVars(s string, sceneVars, panelVars map[string]string) string {
	if len(sceneVars) == 0 && len(panelVars) == 0 {
		return s
	}
	merged := make(map[string]string, len(sceneVars)+len(panelVars))
	for k, v := range sceneVars {
		merged[k] = v
	}
	for k, v := range panelVars {
		merged[k] = v
	}
	for k, v := range merged {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
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

	spec := ui.Dim(opts.Size+ui.Sep()+opts.Quality)
	var imgData []byte
	start := time.Now()
	if len(opts.Refs) > 0 {
		fmt.Fprintf(os.Stderr, "%s Editing%s%s%s%s\n",
			ui.IconGen,
			ui.Sep(), ui.Bold(opts.Output),
			ui.Sep(), spec+ui.Dim(fmt.Sprintf(" (%d ref(s))", len(opts.Refs))))
		imgData, err = client.Edit(fullPrompt, opts.Refs, opts.Size, opts.Quality)
	} else {
		fmt.Fprintf(os.Stderr, "%s Generating%s%s%s%s\n",
			ui.IconGen, ui.Sep(), ui.Bold(opts.Output), ui.Sep(), spec)
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
	fmt.Fprintf(os.Stderr, "%s Saved%s%s%s\n",
		ui.IconOK, ui.Sep(), opts.Output, ui.Dim(" ("+ui.Dur(time.Since(start))+")"))
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
	index            int
	total            int
	pageNum          int
	scene            string
	prompt           string
	output           string
	size             string
	quality          string
	refs             []string
	prefix           string
	continueFromPage int // resolved lazily at generation time
}

func Batch(client *api.Client, opts BatchOptions) error {
	cfg := opts.Config
	panels := cfg.Panels
	if len(panels) == 0 {
		return fmt.Errorf("no panels defined in config")
	}

	if len(opts.Pages) > 0 {
		panels = filterByPageSet(panels, opts.Pages)
		if len(panels) == 0 {
			return fmt.Errorf("no panels match the requested page list")
		}
	}

	outputDir := filepath.Join(opts.ConfigDir, cfg.OutputDir)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	total := len(panels)
	work, skipped := buildWorkList(panels, cfg, opts, outputDir)

	if opts.DryRun {
		fmt.Fprintf(os.Stderr, "\n%s Dry run%s%s would generate%s%d skipped%s(of %d)\n",
			ui.IconDry, ui.Sep(),
			ui.BoldCyan(fmt.Sprintf("%d", len(work))), ui.Sep(),
			skipped, ui.Sep(), total)
		return nil
	}
	if len(work) == 0 {
		fmt.Fprintf(os.Stderr, "\n%s Nothing to do%s%d skipped (of %d)\n",
			ui.IconOK, ui.Sep(), skipped, total)
		return nil
	}

	parallel := opts.Parallel
	if parallel < 1 {
		parallel = 1
	}

	generated, failed := runWorkList(client, work, opts.StyleFile, outputDir, parallel)

	fmt.Fprintf(os.Stderr, "\n%s %s%s%s%s%s%s\n",
		ui.IconOK,
		ui.BoldGreen(fmt.Sprintf("%d generated", generated)), ui.Sep(),
		ui.Yellow(fmt.Sprintf("%d skipped", skipped)), ui.Sep(),
		ui.BoldRed(fmt.Sprintf("%d failed", failed)),
		ui.Dim(fmt.Sprintf(" (of %d panels)", total)))

	if failed > 0 {
		return fmt.Errorf("%d panel(s) failed", failed)
	}
	return nil
}

func generateOne(client *api.Client, item workItem, styleFile string) error {
	fullPrompt, err := BuildPrompt(item.prompt, styleFile, item.prefix)
	if err != nil {
		return err
	}
	var imgData []byte
	if len(item.refs) > 0 {
		imgData, err = client.Edit(fullPrompt, item.refs, item.size, item.quality)
	} else {
		imgData, err = client.Generate(fullPrompt, item.size, item.quality)
	}
	if err != nil {
		return err
	}
	return os.WriteFile(item.output, imgData, 0644)
}

func runWorkList(client *api.Client, work []workItem, styleFile, outputDir string, parallel int) (generated, failed int) {
	// One channel per page; closed when that page finishes (success or failure).
	pageDone := make(map[int]chan struct{}, len(work))
	for _, item := range work {
		pageDone[item.pageNum] = make(chan struct{})
	}

	var mu sync.Mutex

	runOne := func(item workItem) {
		done := pageDone[item.pageNum]
		defer close(done)

		// Wait for continue dependency, then resolve the ref.
		if item.continueFromPage > 0 {
			if depDone, ok := pageDone[item.continueFromPage]; ok {
				<-depDone
			}
			if img := BestPageImage(outputDir, item.continueFromPage); img != "" {
				item.refs = append(item.refs, img)
			} else {
				fmt.Fprintf(os.Stderr, "%s %s %s continue=%d%sno image found for that page\n",
					fmtIdx(item.index, item.total), ui.IconWarn,
					ui.Bold(fmt.Sprintf("Page %d", item.pageNum)),
					item.continueFromPage, ui.Sep())
			}
		}

		scene := ""
		if item.scene != "" {
			scene = ui.Sep() + ui.Dim(item.scene)
		}
		refs := ""
		if len(item.refs) > 0 {
			refs = ui.Dim(fmt.Sprintf(" (%d ref(s))", len(item.refs)))
		}
		fmt.Fprintf(os.Stderr, "%s %s %s%s%s\n",
			fmtIdx(item.index, item.total), ui.IconGen,
			ui.Bold(fmt.Sprintf("Page %d", item.pageNum)),
			scene, refs)

		start := time.Now()
		if err := generateOne(client, item, styleFile); err != nil {
			mu.Lock()
			fmt.Fprintf(os.Stderr, "  %s %s\n", ui.BoldRed("FAILED"), ui.Dim(err.Error()))
			failed++
			mu.Unlock()
			return
		}
		mu.Lock()
		fmt.Fprintf(os.Stderr, "  %s Saved%s%s%s\n",
			ui.IconOK, ui.Sep(), item.output,
			ui.Dim(" ("+ui.Dur(time.Since(start))+")"))
		generated++
		mu.Unlock()
	}

	if parallel <= 1 {
		for _, item := range work {
			runOne(item)
		}
		return
	}
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
	return
}

func buildWorkList(panels []config.Panel, cfg *config.Config, opts BatchOptions, outputDir string) ([]workItem, int) {
	total := len(panels)
	skipped := 0
	var work []workItem

	for i, panel := range panels {
		idx := i + 1
		prompt := strings.TrimSpace(panel.Prompt)
		if prompt == "" || panel.Scene == "blank" {
			fmt.Fprintf(os.Stderr, "%s %s %s%sblank\n",
				fmtIdx(idx, total), ui.IconSkip,
				ui.Bold(fmt.Sprintf("Page %d", panel.Page)), ui.Sep())
			skipped++
			continue
		}

		prefix, sceneRefs, sceneSize, sceneQuality, skip := resolvePanel(cfg, opts, panel, idx, total)
		if skip {
			skipped++
			continue
		}

		size := firstNonEmpty(opts.Size, sceneSize, cfg.Defaults.Size)
		quality := firstNonEmpty(opts.Quality, sceneQuality, cfg.Defaults.Quality)

		panelCharDescs, panelCharRefs := ResolveCharacters(cfg, panel.Characters, opts.ConfigDir)
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
		for _, r := range panel.Refs {
			path := r
			if !filepath.IsAbs(path) {
				path = filepath.Join(opts.ConfigDir, r)
			}
			panelRefs = append(panelRefs, path)
		}
		allRefs := append(sceneRefs, panelRefs...)

		if HasVersion(outputDir, panel.Page, quality) && !opts.Force {
			fmt.Fprintf(os.Stderr, "%s %s %s%s%s version exists\n",
				fmtIdx(idx, total), ui.IconSkip,
				ui.Bold(fmt.Sprintf("Page %d", panel.Page)), ui.Sep(),
				quality)
			skipped++
			continue
		}

		output := NextVersion(outputDir, panel.Page, quality)

		if opts.DryRun {
			fmt.Fprintf(os.Stderr, "%s %s %s%s%s\n",
				fmtIdx(idx, total), ui.IconDry,
				ui.Bold(fmt.Sprintf("Page %d", panel.Page)), ui.Sep(),
				ui.Dim(filepath.Base(output)+" · "+size+" · "+quality))
			continue
		}

		work = append(work, workItem{
			index:            idx,
			total:            total,
			pageNum:          panel.Page,
			scene:            panel.Scene,
			prompt:           prompt,
			output:           output,
			size:             size,
			quality:          quality,
			refs:             allRefs,
			prefix:           prefix,
			continueFromPage: panel.Continue,
		})
	}
	return work, skipped
}

func fmtIdx(idx, total int) string {
	w := fmt.Sprintf("%d", total)
	return ui.Dim(fmt.Sprintf("[%*d/%s]", len(w), idx, w))
}

func resolvePanel(cfg *config.Config, opts BatchOptions, panel config.Panel, idx, total int) (prefix string, refs []string, size, quality string, skip bool) {
	if panel.Scene == "" {
		return "", nil, "", "", false
	}
	resolved, err := ResolveScene(cfg, panel.Scene, opts.ConfigDir, panel.Vars)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s %s %s%s%v\n",
			fmtIdx(idx, total), ui.IconFail,
			ui.Bold(fmt.Sprintf("Page %d", panel.Page)), ui.Sep(), err)
		return "", nil, "", "", true
	}
	return resolved.Prefix, resolved.Refs, resolved.Size, resolved.Quality, false
}

func filterByPageSet(panels []config.Panel, pages []int) []config.Panel {
	pageSet := make(map[int]bool, len(pages))
	for _, p := range pages {
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

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
