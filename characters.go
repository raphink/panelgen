package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"github.com/raphink/panelgen/internal/config"
)

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
	_, max := scanCharacterVersions(dir, name)
	return filepath.Join(dir, fmt.Sprintf("%s-%d.png", name, max+1))
}

// latestCharacterRef returns the path of the highest-numbered existing
// <name>-N.png in dir, or "" if none exist.
func latestCharacterRef(dir, name string) string {
	best, max := scanCharacterVersions(dir, name)
	if max == 0 {
		return ""
	}
	return best
}

func scanCharacterVersions(dir, name string) (bestPath string, max int) {
	matches, _ := filepath.Glob(filepath.Join(dir, name+"-*.png"))
	for _, m := range matches {
		if sub := charVersionRe.FindStringSubmatch(m); sub != nil {
			if n, err := strconv.Atoi(sub[1]); err == nil && n > max {
				max = n
				bestPath = m
			}
		}
	}
	return bestPath, max
}
