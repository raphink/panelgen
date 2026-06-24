package main

import (
	"sort"

	"github.com/raphink/panelgen/internal/config"
	"github.com/raphink/panelgen/internal/generate"
)

func sortedCharacterNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Characters))
	for name := range cfg.Characters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func nextCharacterVersion(dir, name string) string {
	return generate.NextCharacterVersion(dir, name)
}
