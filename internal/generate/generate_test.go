package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raphink/panelgen/internal/config"
)

// ─── BuildPrompt ─────────────────────────────────────────────────────────────

func TestBuildPrompt_PromptOnly(t *testing.T) {
	got, err := BuildPrompt("draw a fox", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "draw a fox" {
		t.Errorf("got %q", got)
	}
}

func TestBuildPrompt_WithPrefix(t *testing.T) {
	got, err := BuildPrompt("draw a fox", "", "Space setting.")
	if err != nil {
		t.Fatal(err)
	}
	want := "Space setting.\n\ndraw a fox"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildPrompt_WithStyleFile(t *testing.T) {
	f := writeTempFile(t, "style rules")
	got, err := BuildPrompt("draw a fox", f, "")
	if err != nil {
		t.Fatal(err)
	}
	want := "style rules\n\ndraw a fox"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildPrompt_AllThree(t *testing.T) {
	f := writeTempFile(t, "style rules")
	got, err := BuildPrompt("draw a fox", f, "Space setting.")
	if err != nil {
		t.Fatal(err)
	}
	want := "style rules\n\nSpace setting.\n\ndraw a fox"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildPrompt_MissingStyleFile(t *testing.T) {
	_, err := BuildPrompt("draw a fox", "/nonexistent/style.txt", "")
	if err == nil {
		t.Fatal("expected error for missing style file")
	}
}

// ─── NextVersion / HasVersion ─────────────────────────────────────────────────

func TestNextVersion_Empty(t *testing.T) {
	dir := t.TempDir()
	got := NextVersion(dir, 1, "high")
	want := filepath.Join(dir, "page_1_high-1.png")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNextVersion_Increment(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "page_1_high-1.png"))
	touch(t, filepath.Join(dir, "page_1_high-2.png"))
	got := NextVersion(dir, 1, "high")
	want := filepath.Join(dir, "page_1_high-3.png")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHasVersion_False(t *testing.T) {
	dir := t.TempDir()
	if HasVersion(dir, 1, "high") {
		t.Error("expected false for empty dir")
	}
}

func TestHasVersion_True(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "page_1_high-1.png"))
	if !HasVersion(dir, 1, "high") {
		t.Error("expected true")
	}
}

// ─── ResolveScene ─────────────────────────────────────────────────────────────

func TestResolveScene_UnknownScene(t *testing.T) {
	cfg := &config.Config{Scenes: map[string]config.Scene{}}
	_, err := ResolveScene(cfg, "missing", ".", nil)
	if err == nil {
		t.Fatal("expected error for unknown scene")
	}
}

func TestResolveScene_Basic(t *testing.T) {
	cfg := &config.Config{
		Scenes: map[string]config.Scene{
			"space": {PromptPrefix: "Space setting.", Size: "1536x1024", Quality: "medium"},
		},
		Characters: map[string]config.Character{},
	}
	r, err := ResolveScene(cfg, "space", "/base", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Prefix != "Space setting." {
		t.Errorf("prefix: got %q", r.Prefix)
	}
	if r.Size != "1536x1024" {
		t.Errorf("size: got %q", r.Size)
	}
	if r.Quality != "medium" {
		t.Errorf("quality: got %q", r.Quality)
	}
	if len(r.Refs) != 0 {
		t.Errorf("expected no refs, got %v", r.Refs)
	}
}

func TestResolveScene_CharacterDescriptionInPrefix(t *testing.T) {
	cfg := &config.Config{
		Scenes: map[string]config.Scene{
			"s": {Characters: []string{"fox"}, PromptPrefix: "Space setting."},
		},
		Characters: map[string]config.Character{
			"fox": {Prompt: "Clockwork fox in a white space suit."},
		},
	}
	r, err := ResolveScene(cfg, "s", ".", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `Character "fox": Clockwork fox in a white space suit.` + "\n\nSpace setting."
	if r.Prefix != want {
		t.Errorf("prefix: got %q, want %q", r.Prefix, want)
	}
}

func TestResolveScene_MultipleCharacterDescriptions(t *testing.T) {
	cfg := &config.Config{
		Scenes: map[string]config.Scene{
			"s": {Characters: []string{"fox", "wolf"}},
		},
		Characters: map[string]config.Character{
			"fox":  {Prompt: "Clockwork fox."},
			"wolf": {Prompt: "Steel wolf."},
		},
	}
	r, err := ResolveScene(cfg, "s", ".", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Prefix, `Character "fox": Clockwork fox.`) {
		t.Errorf("missing fox in prefix: %q", r.Prefix)
	}
	if !strings.Contains(r.Prefix, `Character "wolf": Steel wolf.`) {
		t.Errorf("missing wolf in prefix: %q", r.Prefix)
	}
}

func TestResolveScene_NoDescriptionNoChange(t *testing.T) {
	cfg := &config.Config{
		Scenes: map[string]config.Scene{
			"s": {Characters: []string{"fox"}, PromptPrefix: "Space setting."},
		},
		Characters: map[string]config.Character{
			"fox": {Prompt: ""},
		},
	}
	r, err := ResolveScene(cfg, "s", ".", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Prefix != "Space setting." {
		t.Errorf("prefix: got %q", r.Prefix)
	}
}

func TestResolveScene_CharacterRefsDeduped(t *testing.T) {
	dir := t.TempDir()
	ref := filepath.Join(dir, "fox.png")
	touch(t, ref)

	cfg := &config.Config{
		Scenes: map[string]config.Scene{
			"s": {Characters: []string{"fox"}, Refs: []string{"fox.png"}},
		},
		Characters: map[string]config.Character{
			"fox": {Refs: []string{"fox.png"}},
		},
	}
	r, err := ResolveScene(cfg, "s", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	// fox.png appears in both character refs and scene refs — should be deduplicated
	if len(r.Refs) != 1 {
		t.Errorf("expected 1 ref after dedup, got %d: %v", len(r.Refs), r.Refs)
	}
}

// ─── filterByPageSet ─────────────────────────────────────────────────────────

func TestFilterByPageSet(t *testing.T) {
	panels := []config.Panel{
		{Page: 1}, {Page: 2}, {Page: 3}, {Page: 4},
	}
	got := filterByPageSet(panels, []int{1, 3})
	if len(got) != 2 || got[0].Page != 1 || got[1].Page != 3 {
		t.Errorf("got pages %v", pages(got))
	}
}

func TestFilterByPageSet_Empty(t *testing.T) {
	panels := []config.Panel{{Page: 1}}
	got := filterByPageSet(panels, []int{99})
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

// ─── applyVars ───────────────────────────────────────────────────────────────

func TestApplyVars_SceneDefaults(t *testing.T) {
	got := applyVars("Panel in {setting}.", map[string]string{"setting": "a forest"}, nil)
	if got != "Panel in a forest." {
		t.Errorf("got %q", got)
	}
}

func TestApplyVars_PanelOverride(t *testing.T) {
	got := applyVars("Panel in {setting}.",
		map[string]string{"setting": "a forest"},
		map[string]string{"setting": "a cave"})
	if got != "Panel in a cave." {
		t.Errorf("got %q", got)
	}
}

func TestApplyVars_NoVars(t *testing.T) {
	got := applyVars("No placeholders.", nil, nil)
	if got != "No placeholders." {
		t.Errorf("got %q", got)
	}
}

func TestApplyVars_UnreplacedPlaceholder(t *testing.T) {
	got := applyVars("Hello {unknown}.", map[string]string{"other": "x"}, nil)
	if got != "Hello {unknown}." {
		t.Errorf("got %q", got)
	}
}

// ─── firstNonEmpty ────────────────────────────────────────────────────────────

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "", "c"); got != "c" {
		t.Errorf("got %q", got)
	}
	if got := firstNonEmpty("a", "b"); got != "a" {
		t.Errorf("got %q", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("got %q", got)
	}
}

// ─── MergeCharPrefix ─────────────────────────────────────────────────────────

func TestMergeCharPrefix_EmptyDescs(t *testing.T) {
	if got := MergeCharPrefix("existing", nil); got != "existing" {
		t.Errorf("got %q", got)
	}
}

func TestMergeCharPrefix_EmptyPrefix(t *testing.T) {
	got := MergeCharPrefix("", []string{"desc1", "desc2"})
	want := "desc1\n\ndesc2"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMergeCharPrefix_Appends(t *testing.T) {
	got := MergeCharPrefix("scene prefix", []string{"char desc"})
	want := "scene prefix\n\nchar desc"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// ─── AbsRefs ─────────────────────────────────────────────────────────────────

func TestAbsRefs_RelativeAndAbsolute(t *testing.T) {
	got := AbsRefs([]string{"rel/path.png", "/abs/path.png"}, "/base")
	want := []string{"/base/rel/path.png", "/abs/path.png"}
	if len(got) != len(want) {
		t.Fatalf("len %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestAbsRefs_Empty(t *testing.T) {
	if got := AbsRefs(nil, "/base"); len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

// ─── BestPageImage ────────────────────────────────────────────────────────────

func TestBestPageImage_NoFiles(t *testing.T) {
	dir := t.TempDir()
	if got := BestPageImage(dir, 1); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestBestPageImage_PrefersHighQuality(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir+"/page_1_low-1.png")
	touch(t, dir+"/page_1_medium-1.png")
	touch(t, dir+"/page_1_high-1.png")
	got := BestPageImage(dir, 1)
	if filepath.Base(got) != "page_1_high-1.png" {
		t.Errorf("got %q, want page_1_high-1.png", got)
	}
}

func TestBestPageImage_PrefersHigherVersion(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir+"/page_2_high-1.png")
	touch(t, dir+"/page_2_high-3.png")
	touch(t, dir+"/page_2_high-2.png")
	got := BestPageImage(dir, 2)
	if filepath.Base(got) != "page_2_high-3.png" {
		t.Errorf("got %q, want page_2_high-3.png", got)
	}
}

func TestBestPageImage_QualityBeatsVersion(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir+"/page_3_low-99.png")
	touch(t, dir+"/page_3_high-1.png")
	got := BestPageImage(dir, 3)
	if filepath.Base(got) != "page_3_high-1.png" {
		t.Errorf("got %q, want page_3_high-1.png", got)
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.txt")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(content)
	f.Close()
	return f.Name()
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
}

func pages(panels []config.Panel) []int {
	out := make([]int, len(panels))
	for i, p := range panels {
		out[i] = p.Page
	}
	return out
}
