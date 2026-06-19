// Package config loads and validates panelgen YAML configuration files.
// It implements a lightweight parser for the panelgen schema without
// external dependencies.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Config is the top-level panelgen configuration.
type Config struct {
	Style      string
	OutputDir  string
	Defaults   Defaults
	Scenes     map[string]Scene
	Characters map[string]Character
	Panels     []Panel
}

type Defaults struct {
	Size    string
	Quality string
}

type Character struct {
	Description string
	Refs        []string
}

type Scene struct {
	Description  string
	PromptPrefix string
	Characters   []string
	Refs         []string
	Size         string
	Quality      string
}

type Panel struct {
	Page   int
	Scene  string
	Prompt string
	Refs   []string
}

// Load reads a panelgen YAML config file.
// It uses a hand-rolled parser suited to the known schema rather than
// pulling in an external YAML library.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	lines := []string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	return parse(lines)
}

// ─── Parser ──────────────────────────────────────────────────────────────────

type parser struct {
	lines []string
	pos   int
}

func parse(lines []string) (*Config, error) {
	p := &parser{lines: lines}
	cfg := &Config{
		Scenes:     make(map[string]Scene),
		Characters: make(map[string]Character),
	}

	for p.pos < len(p.lines) {
		line := p.lines[p.pos]
		key, val, ok := topLevelKV(line)
		if !ok {
			p.pos++
			continue
		}

		switch key {
		case "style":
			cfg.Style = val
			p.pos++
		case "output_dir":
			cfg.OutputDir = strings.TrimSuffix(val, "/")
			p.pos++
		case "defaults":
			p.pos++
			cfg.Defaults = p.parseDefaults()
		case "characters":
			p.pos++
			cfg.Characters = p.parseCharacters()
		case "scenes":
			p.pos++
			cfg.Scenes = p.parseScenes()
		case "panels":
			p.pos++
			cfg.Panels = p.parsePanels()
		default:
			p.pos++
		}
	}

	// Apply defaults
	if cfg.Defaults.Size == "" {
		cfg.Defaults.Size = "1024x1024"
	}
	if cfg.Defaults.Quality == "" {
		cfg.Defaults.Quality = "high"
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = "generated"
	}

	return cfg, nil
}

func topLevelKV(line string) (string, string, bool) {
	if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") ||
		strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
		return "", "", false
	}
	idx := strings.Index(line, ":")
	if idx < 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	val := strings.TrimSpace(line[idx+1:])
	// Strip inline comments
	if ci := strings.Index(val, " #"); ci >= 0 {
		val = strings.TrimSpace(val[:ci])
	}
	return key, val, true
}

func indent(line string) int {
	n := 0
	for _, c := range line {
		if c == ' ' {
			n++
		} else if c == '\t' {
			n += 2
		} else {
			break
		}
	}
	return n
}

func kvAt(line string, ind int) (string, string, bool) {
	if indent(line) != ind {
		return "", "", false
	}
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") || trimmed == "" {
		return "", "", false
	}
	idx := strings.Index(trimmed, ":")
	if idx < 0 {
		return "", "", false
	}
	key := strings.TrimSpace(trimmed[:idx])
	val := strings.TrimSpace(trimmed[idx+1:])
	return key, val, true
}

func (p *parser) parseDefaults() Defaults {
	d := Defaults{}
	for p.pos < len(p.lines) {
		line := p.lines[p.pos]
		if indent(line) == 0 && strings.TrimSpace(line) != "" && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			break
		}
		key, val, ok := kvAt(line, 2)
		if !ok {
			p.pos++
			continue
		}
		switch key {
		case "size":
			d.Size = val
		case "quality":
			d.Quality = val
		}
		p.pos++
	}
	return d
}

func (p *parser) parseCharacters() map[string]Character {
	chars := make(map[string]Character)
	for p.pos < len(p.lines) {
		line := p.lines[p.pos]
		if indent(line) == 0 && strings.TrimSpace(line) != "" && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			break
		}
		// Character name at indent 2
		key, _, ok := kvAt(line, 2)
		if !ok {
			p.pos++
			continue
		}
		p.pos++
		char := p.parseCharacter()
		chars[key] = char
	}
	return chars
}

func (p *parser) parseCharacter() Character {
	char := Character{}
	for p.pos < len(p.lines) {
		line := p.lines[p.pos]
		ind := indent(line)
		if ind < 4 && strings.TrimSpace(line) != "" && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			break
		}
		key, val, ok := kvAt(line, 4)
		if !ok {
			p.pos++
			continue
		}
		switch key {
		case "description":
			char.Description = p.parseScalarOrBlock(val, 4)
		case "refs":
			p.pos++
			char.Refs = p.parseStringList(6)
			continue
		}
		p.pos++
	}
	return char
}

func (p *parser) parseScenes() map[string]Scene {
	scenes := make(map[string]Scene)
	for p.pos < len(p.lines) {
		line := p.lines[p.pos]
		if indent(line) == 0 && strings.TrimSpace(line) != "" && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			break
		}
		key, _, ok := kvAt(line, 2)
		if !ok {
			p.pos++
			continue
		}
		p.pos++
		scene := p.parseScene()
		scenes[key] = scene
	}
	return scenes
}

func (p *parser) parseScene() Scene {
	scene := Scene{}
	for p.pos < len(p.lines) {
		line := p.lines[p.pos]
		ind := indent(line)
		if ind < 4 && strings.TrimSpace(line) != "" && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			break
		}
		key, val, ok := kvAt(line, 4)
		if !ok {
			p.pos++
			continue
		}
		switch key {
		case "description":
			scene.Description = p.parseScalarOrBlock(val, 4)
		case "prompt_prefix":
			scene.PromptPrefix = p.parseScalarOrBlock(val, 4)
		case "size":
			scene.Size = val
			p.pos++
		case "quality":
			scene.Quality = val
			p.pos++
		case "characters":
			p.pos++
			scene.Characters = p.parseStringList(6)
		case "refs":
			p.pos++
			scene.Refs = p.parseStringList(6)
		default:
			p.pos++
		}
	}
	return scene
}

func (p *parser) parsePanels() []Panel {
	var panels []Panel
	for p.pos < len(p.lines) {
		line := p.lines[p.pos]
		trimmed := strings.TrimSpace(line)
		if indent(line) == 0 && trimmed != "" && !strings.HasPrefix(trimmed, "#") && trimmed != "-" {
			break
		}
		// Panel starts with "  - page:"
		if strings.HasPrefix(strings.TrimSpace(line), "- ") || strings.TrimSpace(line) == "-" {
			panel := p.parsePanel()
			if panel.Page > 0 {
				panels = append(panels, panel)
			}
			continue
		}
		p.pos++
	}
	return panels
}

func (p *parser) parsePanel() Panel {
	panel := Panel{}
	started := false
	for p.pos < len(p.lines) {
		line := p.lines[p.pos]
		trimmed := strings.TrimSpace(line)
		ind := indent(line)

		// Panel header line, e.g. "- page: 1" or "-"
		if ind == 2 && strings.HasPrefix(trimmed, "- ") {
			if started {
				break
			}
			started = true
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			if rest == "" {
				p.pos++
				continue
			}
			idx := strings.Index(rest, ":")
			if idx < 0 {
				p.pos++
				continue
			}
			key := strings.TrimSpace(rest[:idx])
			val := strings.TrimSpace(rest[idx+1:])
			switch key {
			case "page":
				fmt.Sscanf(val, "%d", &panel.Page)
			case "scene":
				panel.Scene = val
			case "prompt":
				panel.Prompt = p.parseScalarOrBlock(val, 2)
				continue
			case "refs":
				p.pos++
				panel.Refs = p.parseStringList(4)
				continue
			}
			p.pos++
			continue
		}

		// Next panel or top-level key
		if (started && ind == 2 && strings.HasPrefix(trimmed, "- ")) ||
			(ind == 0 && trimmed != "" && !strings.HasPrefix(trimmed, "#")) {
			break
		}

		if !started {
			p.pos++
			continue
		}

		key, val, ok := kvAt(line, 4)
		if !ok {
			p.pos++
			continue
		}
		switch key {
		case "page":
			fmt.Sscanf(val, "%d", &panel.Page)
			p.pos++
		case "scene":
			panel.Scene = val
			p.pos++
		case "prompt":
			panel.Prompt = p.parseScalarOrBlock(val, 4)
		case "refs":
			p.pos++
			panel.Refs = p.parseStringList(6)
		default:
			p.pos++
		}
	}
	panel.Prompt = strings.TrimSpace(panel.Prompt)
	return panel
}

// parseScalarOrBlock handles both inline values and YAML block scalars (> and |).
func (p *parser) parseScalarOrBlock(val string, keyIndent int) string {
	if val == ">" || val == "|" {
		fold := val == ">"
		p.pos++
		var parts []string
		for p.pos < len(p.lines) {
			line := p.lines[p.pos]
			if strings.TrimSpace(line) == "" {
				parts = append(parts, "")
				p.pos++
				continue
			}
			if indent(line) <= keyIndent && strings.TrimSpace(line) != "" {
				break
			}
			parts = append(parts, strings.TrimSpace(line))
			p.pos++
		}
		if fold {
			return strings.Join(parts, " ")
		}
		return strings.Join(parts, "\n")
	}
	p.pos++
	return val
}

// parseStringList reads a YAML list of strings at the given indent level.
func (p *parser) parseStringList(ind int) []string {
	var items []string
	for p.pos < len(p.lines) {
		line := p.lines[p.pos]
		if strings.TrimSpace(line) == "" {
			p.pos++
			continue
		}
		if indent(line) != ind {
			break
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- ") {
			break
		}
		item := strings.TrimPrefix(trimmed, "- ")
		item = strings.Trim(item, `"'`)
		items = append(items, item)
		p.pos++
	}
	return items
}
