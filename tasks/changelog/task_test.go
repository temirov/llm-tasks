package changelog_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/temirov/llm-tasks/internal/pipeline"
	changelog "github.com/temirov/llm-tasks/tasks/changelog"
)

const cfgYAML = `
task: changelog
llm:
  model: gpt-5-mini
  temperature: 0.2
  max_tokens: 1200
inputs:
  - name: version
    required: true
    type: string
    default: ""
    conflicts_with: ["date"]
  - name: date
    required: true
    type: date
    default: ""
    conflicts_with: ["version"]
  - name: git_log
    required: true
    source: stdin
    type: string
recipe:
  system: "Output valid Markdown only."
  format:
    heading: "## [${version}] - ${date}"
    sections:
      - { title: "Highlights", min: 1, max: 3 }
      - { title: "Features ✨" }
      - { title: "Improvements ⚙️" }
      - { title: "Docs 📚" }
      - { title: "CI & Maintenance" }
    footer: "**Upgrade notes:** No breaking changes."
  rules:
    - "Only use information present in the git log."
apply:
  output_path: "./CHANGELOG.md"
  mode: "prepend"
  ensure_blank_line: true
`

// --- helpers ---------------------------------------------------------------

func withTempFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return p
}

func withWorkdir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	return tmp
}

type mockLLM struct{ resp string }

func (m mockLLM) Chat(ctx context.Context, req pipeline.LLMRequest) (pipeline.LLMResponse, error) {
	return pipeline.LLMResponse{RawText: m.resp}, nil
}

func withStdin(t *testing.T, s string) func() {
	t.Helper()
	old := os.Stdin
	pr, pw, _ := os.Pipe()
	go func() { _, _ = pw.Write([]byte(s)); _ = pw.Close() }()
	os.Stdin = pr
	return func() { os.Stdin = old }
}

// --- tests -----------------------------------------------------------------

func TestChangelog_HappyPath_Prepend_Sandboxed(t *testing.T) {
	// Use a private working dir for the test run.
	tmp := withWorkdir(t)

	// Point output_path to an absolute file inside tmp.
	absOut := filepath.Join(tmp, "CHANGELOG.md")
	cfg := strings.ReplaceAll(cfgYAML, `output_path: "./CHANGELOG.md"`, `output_path: "`+absOut+`"`)

	cfgPath := withTempFile(t, "task.changelog.yaml", cfg)
	restore := withStdin(t, "feat: add cool thing (#123) abcd123\n")
	defer restore()

	task, err := changelog.NewFromYAML(cfgPath)
	if err != nil {
		t.Fatalf("NewFromYAML: %v", err)
	}
	task.SetInputs(map[string]string{
		"version": "1.2.3",
		"date":    "2025-01-05",
	})

	// Mock LLM returns a valid section with all configured headings.
	md := strings.TrimSpace(`
## [1.2.3] - 2025-01-05

### Highlights

- Shiny feature for users (#123, abcd123)

### Features ✨

- Initial implementation

### Improvements ⚙️

- Minor refactors

### Docs 📚

- Updated README

### CI & Maintenance

- Bump actions

**Upgrade notes:** No breaking changes.
`)

	runner := pipeline.Runner{
		Client: mockLLM{resp: md},
		Options: pipeline.RunOptions{
			MaxAttempts: 1,
			Timeout:     5 * time.Second,
		},
	}

	_, err = runner.Run(context.Background(), task)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Assert the file was written where we asked, and starts with our section + blank line.
	b, err := os.ReadFile(absOut)
	if err != nil {
		t.Fatalf("read %s: %v", absOut, err)
	}
	wantPrefix := md + "\n\n"
	if !bytes.HasPrefix(b, []byte(wantPrefix)) {
		t.Fatalf("CHANGELOG.md doesn't start with expected section")
	}

	// Also ensure nothing leaked into the repo root.
	if _, err := os.Stat(filepath.Join("tasks", "changelog", "CHANGELOG.md")); err == nil {
		t.Fatalf("unexpected file in repo: tasks/changelog/CHANGELOG.md")
	}
}

func TestChangelog_Verify_RefinesOnMissingSection(t *testing.T) {
	tmp := withWorkdir(t)
	absOut := filepath.Join(tmp, "CHANGELOG.md")
	cfg := strings.ReplaceAll(cfgYAML, `output_path: "./CHANGELOG.md"`, `output_path: "`+absOut+`"`)
	cfgPath := withTempFile(t, "task.changelog.yaml", cfg)
	restore := withStdin(t, "fix: stuff\n")
	defer restore()

	task, err := changelog.NewFromYAML(cfgPath)
	if err != nil {
		t.Fatalf("NewFromYAML: %v", err)
	}
	task.SetInputs(map[string]string{
		"version": "0.9.0",
		"date":    "2025-02-01",
	})

	// Missing "CI & Maintenance" on purpose
	md := strings.TrimSpace(`
## [0.9.0] - 2025-02-01

### Highlights

- One highlight

### Features ✨

### Improvements ⚙️

### Docs 📚

**Upgrade notes:** No breaking changes.
`)

	g, err := task.Gather(context.Background())
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	_, err = task.Prompt(context.Background(), g)
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}

	ok, _, refine, verr := task.Verify(context.Background(), g, pipeline.LLMResponse{RawText: md})
	if verr != nil {
		t.Fatalf("verify err: %v", verr)
	}
	if ok || refine == nil || !strings.Contains(refine.UserPromptDelta, "CI & Maintenance") {
		t.Fatalf("expected refine for missing section, got ok=%v refine=%v", ok, refine)
	}
}

func TestChangelog_FallbackWhenLLMReturnsEmpty(t *testing.T) {
	tmp := withWorkdir(t)
	absOut := filepath.Join(tmp, "CHANGELOG.md")
	cfg := strings.ReplaceAll(cfgYAML, `output_path: "./CHANGELOG.md"`, `output_path: "`+absOut+`"`)
	cfgPath := withTempFile(t, "task.changelog.yaml", cfg)
	restore := withStdin(t, "Commits v0.1.0..HEAD:\n58e06a8 feat: add API\n07a7c2b docs: update README\n")
	defer restore()

	task, err := changelog.NewFromYAML(cfgPath)
	if err != nil {
		t.Fatalf("NewFromYAML: %v", err)
	}
	task.SetInputs(map[string]string{
		"version": "1.2.3",
		"date":    "2025-01-05",
	})
	if _, err := task.Gather(context.Background()); err != nil {
		t.Fatalf("gather: %v", err)
	}

	accepted, output, _, err := task.Verify(context.Background(), nil, pipeline.LLMResponse{RawText: ""})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !accepted {
		t.Fatalf("expected fallback verification to accept output")
	}
	fallback := output.(string)
	if !strings.Contains(fallback, "## [1.2.3] - 2025-01-05") {
		t.Fatalf("expected fallback to include heading, got %s", fallback)
	}
	if !strings.Contains(fallback, "feat: add API") {
		t.Fatalf("expected fallback to carry commit message, got %s", fallback)
	}
}
