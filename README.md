# panelgen

AI image series generator. Define scenes and panels in YAML, generate consistent image series with gpt-image-2 via Azure AI Foundry.

Single static binary. No runtime dependencies. Designed to drop cleanly into agentic pipelines.

## Install

**Binary (recommended):**
```bash
# Download from GitHub releases
curl -L https://github.com/raphink/panelgen/releases/latest/download/panelgen-linux-amd64 \
  -o panelgen && chmod +x panelgen
```

**From source:**
```bash
go install github.com/raphink/panelgen@latest
```

**Container:**
```bash
# Azure
docker run --rm \
  -v "$PWD:/work" \
  -e AZURE_OPENAI_ENDPOINT \
  -e AZURE_OPENAI_API_KEY \
  panelgen batch --config panelgen.yml

# OpenAI
docker run --rm \
  -v "$PWD:/work" \
  -e OPENAI_API_KEY \
  panelgen batch --config panelgen.yml
```

## Requirements

**Azure OpenAI:**
```bash
export AZURE_OPENAI_ENDPOINT="https://your-resource.openai.azure.com"
export AZURE_OPENAI_API_KEY="your-key"
export AZURE_OPENAI_DEPLOYMENT="gpt-image-2"   # optional, default: gpt-image-2
```

**Standard OpenAI:**
```bash
export OPENAI_API_KEY="your-key"
export OPENAI_MODEL="gpt-image-2"    # optional, default: gpt-image-2
export OPENAI_BASE_URL="https://api.openai.com"  # optional, for custom endpoints
```

Provider is selected automatically: Azure when `AZURE_OPENAI_ENDPOINT` is set, otherwise OpenAI.

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
```

### List scenes

```bash
panelgen scenes
panelgen scenes -config comic.yml
```

### Starter example files

```bash
# Copy starter config and style guide
cp examples/panelgen.yml ./panelgen.yml
cp examples/style.txt ./style.txt

# Optional: add your reference images
mkdir -p refs
# cp /path/to/your/reference.png refs/clockwork-fox.png

# Generate panels
panelgen batch --config panelgen.yml
```

## Config format

```yaml
style: style.txt          # Style guide prepended to every prompt

defaults:
  size: 1024x1024
  quality: low

output_dir: generated/

characters:
  astronaut:
    description: "Clockwork fox explorer — white space suit, glass helmet"
    refs:
      - characters/clockwork-fox.png

scenes:
  space-solo:
    description: "Single character floating in space"
    prompt_prefix: >
      Comic panel in outer space with purple/blue starry background.
      Square panel with rounded corners.
    characters:
      - astronaut
    size: 1024x1024

panels:
  - page: 1
    scene: space-solo
    prompt: >
      Character floating near a terminal. Speech bubble: "Nice. The app is up."

  - page: 2
    scene: space-solo
    prompt: >
      Character looking shocked at an exploding pod.
```

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
  -e AZURE_OPENAI_ENDPOINT \
  -e AZURE_OPENAI_API_KEY \
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

MIT
