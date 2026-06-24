# panelgen

[![checks](https://github.com/raphink/panelgen/actions/workflows/checks.yml/badge.svg?branch=main)](https://github.com/raphink/panelgen/actions/workflows/checks.yml)
[![release](https://github.com/raphink/panelgen/actions/workflows/release.yml/badge.svg)](https://github.com/raphink/panelgen/actions/workflows/release.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/raphink/panelgen)](https://github.com/raphink/panelgen/blob/main/go.mod)
[![Go Report Card](https://goreportcard.com/badge/github.com/raphink/panelgen)](https://goreportcard.com/report/github.com/raphink/panelgen)
[![Go Reference](https://pkg.go.dev/badge/github.com/raphink/panelgen.svg)](https://pkg.go.dev/github.com/raphink/panelgen)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

AI image series generator. Define scenes and panels in YAML, generate consistent image series with gpt-image-2 via Azure AI Foundry.

Single static binary. No runtime dependencies. Designed to drop cleanly into agentic pipelines.

## Install

**Homebrew (recommended):**
```bash
brew tap raphink/tap
brew install panelgen
```

**From source:**
```bash
go install github.com/raphink/panelgen@latest
```

**Binary:**
```bash
# Download from GitHub releases
curl -L https://github.com/raphink/panelgen/releases/latest/download/panelgen-linux-amd64 \
  -o panelgen && chmod +x panelgen
```

**Container:**
```bash
docker run --rm \
  -v "$PWD:/work" \
  -e OPENAI_API_KEY \
  -e OPENAI_BASE_URL \
  panelgen batch --config panelgen.yml
```

## Requirements

```bash
export OPENAI_API_KEY="your-key"
export OPENAI_BASE_URL="https://api.openai.com/v1"  # default; set to Azure endpoint for Azure
export OPENAI_MODEL="gpt-image-2"                   # optional, default: gpt-image-2
```

For Azure Foundry, set `OPENAI_BASE_URL` to your Azure OpenAI endpoint, e.g.:
```bash
export OPENAI_BASE_URL="https://your-resource.openai.azure.com/openai/v1"
```

## Usage

### Generate a single image

```bash
# Inline prompt
panelgen generate "A clockwork fox in a space suit floating near a Kubernetes cluster" output.png

# Prompt from file — useful for long prompts or agentic pipelines
panelgen generate -prompt-file prompt.txt output.png

# With a scene from your config (adds refs, prompt prefix, size defaults)
panelgen generate -scene space-solo -prompt-file prompt.txt output.png

# With reference images
panelgen generate "Same character, different pose" output.png -ref previous.png

# Size and quality overrides
panelgen generate "..." output.png -size 1536x1024 -quality medium

# Outpainting: expand a square image to landscape
panelgen generate --quality low --ref square_img.jpg --size 1920x1088 'outpaint the image' landscape_img.jpg
```

### Batch generation

```bash
# Generate all panels in panelgen.yml
panelgen batch

# Specific config file
panelgen batch -config comic.yml

# Dry run
panelgen batch -dry-run

# Specific pages
panelgen batch -pages 1,3,5-10

# Force a new version even if output exists
panelgen batch -force

# Parallel generation
panelgen batch -parallel 4

# Quality override
panelgen batch -quality high

# Assemble PDF after generation
panelgen batch --assemble
```

### Lint config

```bash
# Validate config structure and local file references
panelgen lint --config panelgen.yml

# Fail on warnings too
panelgen lint --config panelgen.yml --strict
```

### Plan / preview

```bash
# Preview what would be generated without calling the image API
panelgen plan --config panelgen.yml

# Include fully-resolved prompt text and all refs
panelgen plan --config panelgen.yml --show-prompt --show-refs
```

### List scenes

```bash
panelgen scenes
panelgen scenes -config comic.yml
```

### Character reference images

```bash
# List characters and their prompts
panelgen characters list

# Generate a specific character
panelgen characters generate explorer

# Generate all characters
panelgen characters generate --all

# Override output directory, size, or quality
panelgen characters generate --all --output-dir refs/chars --quality low
```

Output files are versioned: `characters/explorer-1.png`, `characters/explorer-2.png`, etc.
The output directory defaults to `characters/` next to the config, or `defaults.characters_dir` if set.

Once character images exist, `batch` and `plan` automatically use the latest generated image as the
character ref in panel prompts — no YAML change needed. `characters generate` always uses the
original `refs:` from the config to regenerate from the source concept.

### Starter example files

```bash
# Copy starter config and style guide
cp examples/panelgen.yml ./panelgen.yml
cp examples/style.txt ./style.txt

# Advanced flow examples (panel-to-panel continuity, panel-specific refs)
cp examples/panelgen-advanced.yml ./panelgen-advanced.yml

# Optional: add your reference images
mkdir -p refs
# cp /path/to/your/reference.png refs/clockwork-fox.png

# Generate panels
panelgen batch --config panelgen.yml
```

`examples/panelgen-advanced.yml` demonstrates:
- `continue: N` to carry the best generated image for page N as a ref into the next panel
- panel-level `characters:` to add a character for one panel without changing scene defaults
- scene `vars:` with defaults overridden per-panel via `{placeholder}` substitution

## Config format

```yaml
imports:
  - ../shared/characters.yml   # Merge characters, scenes, style, and defaults from other files
  - ../shared/scenes.yml       # Importing file's values take precedence on conflict

style: style.txt          # Style guide prepended to every prompt

defaults:
  size: 1024x1024         # Any WxH where both dims are divisible by 16 and total ≤ 8,294,400 px
  quality: low
  assemble: true          # Automatically assemble a PDF after every batch run
  characters_dir: refs/chars          # Output directory for generated character images (default: characters/)
  characters_preprompt: "..."         # Preprompt for character generation (default: solid-background reference sheet)

output_dir: generated/

characters:
  explorer:
    prompt: "Clockwork fox explorer — white space suit, glass helmet"
    refs:
      - characters/explorer-1.png

scenes:
  space-solo:
    description: "Single character floating in space"
    prompt_prefix: >
      Comic panel in outer space with purple/blue starry background.
      {framing}
    vars:
      framing: "Square panel with rounded corners."
    characters:
      - explorer
    size: 1024x1024

panels:
  - page: 1
    scene: space-solo
    prompt: >
      Character floating near a terminal. Speech bubble: "Nice. The app is up."

  - page: 2
    scene: space-solo
    continue: 1   # passes the best generated image for page 1 as a reference
    vars:
      framing: "Wide shot, character small against the void."
    prompt: >
      Character looking shocked at an exploding pod.
```

`{placeholder}` values in `prompt_prefix` are substituted at generation time. Scene `vars:` provide defaults; panel `vars:` override them per-panel.

### Output versioning

Each panel is saved as `page_{N}_{quality}-{version}.png` (e.g. `page_3_low-1.png`).
Re-running skips panels that already have a version at the requested quality.
Use `-force` to generate a new increment without deleting existing versions.

### Agentic use

Because `panelgen` is a static binary with no runtime dependencies, it drops into
any agentic pipeline as a shell tool:

```bash
# Generate a prompt with an LLM, pass it to panelgen
llm "Write a comic panel prompt for page 5" > prompt.txt
panelgen generate -prompt-file prompt.txt -scene space-conversation output.png
```

Or via container in a CI/agentic step:

```bash
docker run --rm \
  -v "$PWD:/work" \
  -e OPENAI_API_KEY \
  -e OPENAI_BASE_URL \
  panelgen generate -prompt-file /work/prompt.txt /work/output.png
```

## Building

```bash
make build        # current platform
make build-all    # Linux amd64/arm64, macOS amd64/arm64, Windows amd64
make docker       # container image
```

## Release

Releases are automated with GoReleaser.

```bash
make release-check     # validate .goreleaser.yml
make release-snapshot  # local artifacts in dist/ (no GitHub publish)
```

Push a version tag to publish a GitHub release via Actions:

```bash
git tag v0.1.0
git push origin v0.1.0
```

## License

Apache 2.0
