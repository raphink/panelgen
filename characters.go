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
