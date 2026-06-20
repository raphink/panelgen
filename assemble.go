package main

import (
	"flag"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/signintech/gopdf"

	"github.com/raphink/panelgen/internal/config"
)

var qualityRank = map[string]int{"high": 3, "medium": 2, "low": 1}

var versionedRe = regexp.MustCompile(`^page_(\d+)_(low|medium|high)-(\d+)\.png$`)

type pageCandidate struct {
	page      int
	quality   int
	increment int
	path      string
}

func cmdAssemble(args []string) {
	fs := flag.NewFlagSet("assemble", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: panelgen assemble [options]

Assemble generated page images into a PDF.
For each page, picks the highest quality then latest increment.

OPTIONS
`)
		fs.PrintDefaults()
	}

	configFile := fs.String("config", defaultConfig, "Config `FILE` for output_dir and 'selected' overrides")
	inputDir := fs.String("input", "", "Directory containing page images (default: output_dir from config)")
	output := fs.String("output", "", "Output PDF path (default: <config-name>.pdf)")
	verbose := fs.Bool("verbose", false, "Show which image is picked for each page")
	listOnly := fs.Bool("list", false, "Show selection without generating PDF")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	cfg, resolvedInput, resolvedOutput := resolveAssemblePaths(*configFile, *inputDir, *output)

	best, err := findBestImages(resolvedInput)
	if err != nil {
		fatalf("find images: %v", err)
	}

	if cfg != nil {
		best = applySelectedOverrides(cfg, resolvedInput, best)
	}

	if len(best) == 0 {
		fatalf("no page images found in %s", *inputDir)
	}

	for _, c := range best {
		if *verbose || *listOnly {
			fmt.Printf("  Page %3d: %s\n", c.page, filepath.Base(c.path))
		}
	}

	if *listOnly {
		return
	}

	if err := assemblePDF(best, resolvedOutput); err != nil {
		fatalf("assemble PDF: %v", err)
	}
	fmt.Printf("Assembled %d pages -> %s\n", len(best), resolvedOutput)
}

func resolveAssemblePaths(configFile, inputDir, output string) (*config.Config, string, string) {
	var cfg *config.Config
	configDir := "."
	if _, err := os.Stat(configFile); err == nil {
		loaded, err := config.Load(configFile)
		if err != nil {
			fatalf("load config: %v", err)
		}
		cfg = loaded
		configDir = filepath.Dir(configFile)
	}

	resolvedInput := inputDir
	if resolvedInput == "" && cfg != nil {
		resolvedInput = filepath.Join(configDir, cfg.OutputDir)
	}
	if resolvedInput == "" {
		resolvedInput = "generated"
	}

	resolvedOutput := output
	if resolvedOutput == "" {
		stem := strings.TrimSuffix(filepath.Base(configFile), filepath.Ext(configFile))
		resolvedOutput = filepath.Join(filepath.Dir(configFile), stem+".pdf")
	}
	return cfg, resolvedInput, resolvedOutput
}

func applySelectedOverrides(cfg *config.Config, inputDir string, best []pageCandidate) []pageCandidate {
	bestMap := make(map[int]string, len(best))
	for _, c := range best {
		bestMap[c.page] = c.path
	}
	for _, panel := range cfg.Panels {
		if panel.Selected == "" {
			continue
		}
		sel := filepath.Join(inputDir, panel.Selected)
		if _, err := os.Stat(sel); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: page %d selected %q not found, using auto\n", panel.Page, panel.Selected)
			continue
		}
		bestMap[panel.Page] = sel
	}
	pages := make([]int, 0, len(bestMap))
	for p := range bestMap {
		pages = append(pages, p)
	}
	sort.Ints(pages)
	result := best[:0]
	for _, p := range pages {
		result = append(result, pageCandidate{page: p, path: bestMap[p]})
	}
	return result
}

func findBestImages(dir string) ([]pageCandidate, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	byPage := map[int][]pageCandidate{}
	for _, e := range entries {
		m := versionedRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		page, _ := strconv.Atoi(m[1])
		quality := m[2]
		increment, _ := strconv.Atoi(m[3])
		byPage[page] = append(byPage[page], pageCandidate{
			page:      page,
			quality:   qualityRank[quality],
			increment: increment,
			path:      filepath.Join(dir, e.Name()),
		})
	}

	pages := make([]int, 0, len(byPage))
	for p := range byPage {
		pages = append(pages, p)
	}
	sort.Ints(pages)

	var best []pageCandidate
	for _, p := range pages {
		candidates := byPage[p]
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].quality != candidates[j].quality {
				return candidates[i].quality > candidates[j].quality
			}
			return candidates[i].increment > candidates[j].increment
		})
		best = append(best, candidates[0])
	}
	return best, nil
}

func assemblePDF(pages []pageCandidate, output string) error {
	pdf := gopdf.GoPdf{}

	for i, c := range pages {
		f, err := os.Open(c.path)
		if err != nil {
			return fmt.Errorf("open %s: %w", c.path, err)
		}
		img, err := png.DecodeConfig(f)
		f.Close()
		if err != nil {
			return fmt.Errorf("decode %s: %w", c.path, err)
		}

		w := float64(img.Width)
		h := float64(img.Height)

		if i == 0 {
			pdf.Start(gopdf.Config{PageSize: gopdf.Rect{W: w, H: h}})
		}

		pdf.AddPageWithOption(gopdf.PageOption{PageSize: &gopdf.Rect{W: w, H: h}})
		if err := pdf.Image(c.path, 0, 0, &gopdf.Rect{W: w, H: h}); err != nil {
			return fmt.Errorf("embed %s: %w", c.path, err)
		}
	}

	return pdf.WritePdf(output)
}
