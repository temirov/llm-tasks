package changelog

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/temirov/llm-tasks/internal/config"
	"github.com/temirov/llm-tasks/internal/pipeline"
)

// Make the task's Config exactly the same type as config.ChangelogConfig.
type Config = config.ChangelogConfig

type Task struct {
	cfg     Config
	version string
	date    string
	gitLog  string
	request pipeline.LLMRequest
	section string
	inputs  map[string]string
	root    string
}

// New provides a zero-arg factory for CLI registry.
// It reads LLMTASKS_CHANGELOG_CONFIG or defaults to configs/task.changelog.yaml.
// If the YAML cannot be loaded, it returns a failTask that surfaces the error on Gather.
func New() pipeline.Pipeline {
	path := strings.TrimSpace(os.Getenv("LLMTASKS_CHANGELOG_CONFIG"))
	if path == "" {
		path = "configs/task.changelog.yaml"
	}
	t, err := NewFromYAML(path)
	if err != nil {
		return failTask{err: fmt.Errorf("changelog config load (%s): %w", path, err)}
	}
	return t
}

func NewFromYAML(yamlPath string) (*Task, error) {
	data, err := os.ReadFile(filepath.Clean(yamlPath))
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Task == "" {
		return nil, errors.New("configs/task.changelog.yaml: task field is required")
	}
	wd, wdErr := os.Getwd()
	if wdErr != nil {
		return nil, wdErr
	}
	return &Task{cfg: cfg, root: wd}, nil
}

func (t *Task) Name() string { return "changelog" }

func (t *Task) SetInputs(values map[string]string) {
	normalized := make(map[string]string, len(values))
	for key, value := range values {
		normalized[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	t.inputs = normalized
}

func (t *Task) SetRoot(root string) error {
	trimmed := strings.TrimSpace(root)
	if trimmed == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		trimmed = wd
	}
	absPath, err := filepath.Abs(trimmed)
	if err != nil {
		return err
	}
	info, statErr := os.Stat(absPath)
	if statErr != nil {
		return statErr
	}
	if !info.IsDir() {
		return fmt.Errorf("root must be a directory: %s", absPath)
	}
	t.root = absPath
	return nil
}

// 1) Gather: version, date, git log (stdin)
func (t *Task) Gather(ctx context.Context) (pipeline.GatherOutput, error) {
	collected := make(map[string]string)
	var stdinLoaded bool
	var stdinValue string

	for _, def := range t.cfg.Inputs {
		nameKey := strings.ToLower(def.Name)
		value := strings.TrimSpace(t.inputs[nameKey])

		if strings.EqualFold(def.Source, "stdin") {
			if !stdinLoaded {
				var buf bytes.Buffer
				if err := readAllToBufferCtx(ctx, os.Stdin, &buf); err != nil {
					return nil, fmt.Errorf("reading stdin: %w", err)
				}
				stdinValue = strings.TrimSpace(buf.String())
				stdinLoaded = true
			}
			value = stdinValue
		} else if value == "" {
			value = strings.TrimSpace(def.Default)
		}

		if def.Required && strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%s is required", def.Name)
		}

		switch def.Type {
		case "date":
			if value != "" {
				if _, err := time.Parse(time.DateOnly, value); err != nil {
					return nil, fmt.Errorf("invalid date for %s: %w", def.Name, err)
				}
			}
		}

		collected[nameKey] = value
	}

	t.version = strings.TrimSpace(collected["version"])
	t.date = strings.TrimSpace(collected["date"])
	t.gitLog = strings.TrimSpace(collected["git_log"])
	if t.gitLog == "" && stdinLoaded {
		t.gitLog = stdinValue
	}
	exclude := t.excludedPaths()
	t.gitLog = normalizeGitLog(t.gitLog, 2000, exclude)

	return map[string]string{
		"version": t.version,
		"date":    t.date,
		"git_log": t.gitLog,
	}, nil
}

// 2) Prompt: build system+user prompts from YAML
func (t *Task) Prompt(ctx context.Context, _ pipeline.GatherOutput) (pipeline.LLMRequest, error) {
	sys := strings.TrimSpace(t.cfg.Recipe.System)
	heading := expandTemplate(t.cfg.Recipe.Format.Heading, map[string]string{
		"version": t.version,
		"date":    t.date,
	})

	var sb strings.Builder
	sb.WriteString("Summarize the following git log into a Markdown changelog section.\n\n")
	sb.WriteString("Format:\n")
	sb.WriteString(heading)
	sb.WriteString("\n\n")
	for _, s := range t.cfg.Recipe.Format.Sections {
		sb.WriteString("### ")
		sb.WriteString(s.Title)
		sb.WriteString("\n\n")
	}
	if foot := strings.TrimSpace(t.cfg.Recipe.Format.Footer); foot != "" {
		sb.WriteString(foot)
		sb.WriteString("\n\n")
	}
	sb.WriteString("Rules:\n")
	for _, r := range t.cfg.Recipe.Rules {
		sb.WriteString("- ")
		sb.WriteString(r)
		sb.WriteString("\n")
	}
	sb.WriteString("\nGit log:\n")
	sb.WriteString(t.gitLog)

	t.request = pipeline.LLMRequest{
		SystemPrompt: sys,
		UserPrompt:   sb.String(),
		MaxTokens:    max1(t.cfg.LLM.MaxTokens, 1200),
		Temperature:  t.cfg.LLM.Temperature,
		Model:        t.cfg.LLM.Model,
	}
	return t.request, nil
}

// 3) Verify
func (t *Task) Verify(ctx context.Context, _ pipeline.GatherOutput, response pipeline.LLMResponse) (bool, pipeline.VerifiedOutput, *pipeline.RefineRequest, error) {
	md := strings.TrimSpace(response.RawText)
	if md == "" {
		fallback, ok := t.buildFallbackSection()
		if ok {
			t.section = fallback
			return true, fallback, nil, nil
		}
		return false, nil, &pipeline.RefineRequest{
			UserPromptDelta: fmt.Sprintf("Return a fully formatted changelog starting with %q", strings.TrimSpace(expandTemplate(t.cfg.Recipe.Format.Heading, map[string]string{
				"version": t.version,
				"date":    t.date,
			}))),
			Reason: "empty-response",
		}, nil
	}

	// No code fences
	if strings.Contains(md, "```") {
		return false, nil, &pipeline.RefineRequest{
			UserPromptDelta: "Do not use code fences. Return plain Markdown only.",
			Reason:          "code-fences",
		}, nil
	}

	// Must start with expected heading
	wantPrefix := expandTemplate(t.cfg.Recipe.Format.Heading, map[string]string{
		"version": t.version,
		"date":    t.date,
	})
	if !strings.HasPrefix(md, strings.TrimSpace(wantPrefix)) {
		return false, nil, &pipeline.RefineRequest{
			UserPromptDelta: fmt.Sprintf("Your output must start with the exact heading: %q", strings.TrimSpace(wantPrefix)),
			Reason:          "bad-heading",
		}, nil
	}

	// Ensure each configured section title exists
	for _, s := range t.cfg.Recipe.Format.Sections {
		needle := "### " + s.Title
		if !strings.Contains(md, needle) {
			return false, nil, &pipeline.RefineRequest{
				UserPromptDelta: fmt.Sprintf("Include the section heading %q exactly, even if empty.", needle),
				Reason:          "missing-section",
			}, nil
		}
	}

	// Highlights min constraint (if > 0)
	if len(t.cfg.Recipe.Format.Sections) > 0 && t.cfg.Recipe.Format.Sections[0].Title == "Highlights" && t.cfg.Recipe.Format.Sections[0].Min > 0 {
		highlightsBlock := extractSection(md, "Highlights")
		bullets := countBullets(highlightsBlock)
		if bullets < t.cfg.Recipe.Format.Sections[0].Min {
			return false, nil, &pipeline.RefineRequest{
				UserPromptDelta: fmt.Sprintf("Provide at least %d concise bullets under 'Highlights'.", t.cfg.Recipe.Format.Sections[0].Min),
				Reason:          "too-few-highlights",
			}, nil
		}
	}

	t.section = md
	return true, md, nil, nil
}

func (t *Task) buildFallbackSection() (string, bool) {
	commitMessages := extractCommitMessages(t.gitLog)
	if len(commitMessages) == 0 {
		return "", false
	}
	sectionBuckets := map[string][]string{}
	for _, section := range t.cfg.Recipe.Format.Sections {
		sectionBuckets[section.Title] = []string{}
	}
	sectionOrder := make([]string, 0, len(t.cfg.Recipe.Format.Sections))
	for _, section := range t.cfg.Recipe.Format.Sections {
		sectionOrder = append(sectionOrder, section.Title)
	}

	for idx, message := range commitMessages {
		target := classifyCommit(sectionOrder, message)
		sectionBuckets[target] = append(sectionBuckets[target], message)
		if idx == 0 && len(sectionBuckets[sectionOrder[0]]) == 0 {
			sectionBuckets[sectionOrder[0]] = append(sectionBuckets[sectionOrder[0]], message)
		}
	}

	if len(sectionBuckets[sectionOrder[0]]) == 0 {
		sectionBuckets[sectionOrder[0]] = append(sectionBuckets[sectionOrder[0]], commitMessages[0])
	}

	head := expandTemplate(t.cfg.Recipe.Format.Heading, map[string]string{
		"version": t.version,
		"date":    t.date,
	})
	var builder strings.Builder
	builder.WriteString(strings.TrimSpace(head))
	builder.WriteString("\n\n")
	for _, section := range sectionOrder {
		builder.WriteString("### ")
		builder.WriteString(section)
		builder.WriteString("\n\n")
		bullets := sectionBuckets[section]
		if len(bullets) == 0 {
			builder.WriteString("- _No updates._\n\n")
			continue
		}
		for _, bullet := range bullets {
			builder.WriteString("- ")
			builder.WriteString(bullet)
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}
	footer := strings.TrimSpace(t.cfg.Recipe.Format.Footer)
	if footer != "" {
		builder.WriteString(footer)
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String()), true
}

func extractCommitMessages(gitContext string) []string {
	lines := strings.Split(gitContext, "\n")
	var commits []string
	inCommits := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Diff ") {
			break
		}
		if strings.HasPrefix(trimmed, "Commits ") {
			inCommits = true
			continue
		}
		if !inCommits {
			continue
		}
		if trimmed == "" {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		message := strings.Join(fields[1:], " ")
		commits = append(commits, message)
	}
	return commits
}

func classifyCommit(sectionOrder []string, message string) string {
	lower := strings.ToLower(message)
	for _, section := range sectionOrder {
		switch section {
		case "Features ✨":
			if strings.Contains(lower, "feat") || strings.Contains(lower, "feature") {
				return section
			}
		case "Improvements ⚙️":
			if strings.Contains(lower, "fix") || strings.Contains(lower, "bug") || strings.Contains(lower, "improve") {
				return section
			}
		case "Docs 📚":
			if strings.Contains(lower, "doc") || strings.Contains(lower, "readme") {
				return section
			}
		case "CI & Maintenance":
			if strings.Contains(lower, "ci") || strings.Contains(lower, "refactor") || strings.Contains(lower, "maintenance") || strings.Contains(lower, "chore") {
				return section
			}
		}
	}
	return sectionOrder[0]
}

// 4) Apply: prepend to CHANGELOG.md or print
func (t *Task) Apply(ctx context.Context, verified pipeline.VerifiedOutput) (pipeline.ApplyReport, error) {
	md := verified.(string)
	switch strings.ToLower(t.cfg.Apply.Mode) {
	case "print":
		fmt.Println(md)
		return pipeline.ApplyReport{DryRun: false, Summary: "printed changelog section", NumActions: 1}, nil
	case "prepend":
		path := coalesce(t.cfg.Apply.OutputPath, "./CHANGELOG.md")
		if !filepath.IsAbs(path) {
			base := t.root
			if base == "" {
				wd, err := os.Getwd()
				if err != nil {
					return pipeline.ApplyReport{}, err
				}
				base = wd
			}
			path = filepath.Join(base, path)
		}
		var existing string
		if b, err := os.ReadFile(filepath.Clean(path)); err == nil {
			existing = string(b)
		}
		var out strings.Builder
		out.WriteString(md)
		out.WriteString("\n")
		if t.cfg.Apply.EnsureBlankLine {
			out.WriteString("\n")
		}
		out.WriteString(strings.TrimLeft(existing, "\n"))
		if err := os.WriteFile(filepath.Clean(path), []byte(out.String()), 0o644); err != nil {
			return pipeline.ApplyReport{}, err
		}
		return pipeline.ApplyReport{DryRun: false, Summary: "prepended changelog to " + path, NumActions: 1}, nil
	default:
		return pipeline.ApplyReport{}, fmt.Errorf("unknown apply.mode: %s", t.cfg.Apply.Mode)
	}
}

// --- helpers ---

func readAllToBufferCtx(ctx context.Context, r io.Reader, dst *bytes.Buffer) error {
	sc := bufio.NewScanner(r)
	done := make(chan error, 1)
	go func() {
		for sc.Scan() {
			_, _ = dst.Write(sc.Bytes())
			_, _ = dst.WriteString("\n")
		}
		done <- sc.Err()
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

func extractSection(md, title string) string {
	start := "### " + title
	i := strings.Index(md, start)
	if i < 0 {
		return ""
	}
	rest := md[i+len(start):]
	// Stop at next "### " or end
	j := strings.Index(rest, "\n### ")
	if j >= 0 {
		return rest[:j]
	}
	return rest
}

func countBullets(s string) int {
	c := 0
	for _, line := range strings.Split(s, "\n") {
		lt := strings.TrimSpace(line)
		if strings.HasPrefix(lt, "- ") || strings.HasPrefix(lt, "* ") {
			c++
		}
	}
	return c
}

func expandTemplate(tmpl string, vars map[string]string) string {
	out := tmpl
	for k, v := range vars {
		out = strings.ReplaceAll(out, "${"+k+"}", v)
	}
	return out
}

func coalesce(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func normalizeGitLog(log string, maxTotalRunes int, excludePaths []string) string {
	if maxTotalRunes <= 0 {
		return strings.TrimSpace(log)
	}
	excludeSet := buildExcludeSet(excludePaths)
	commitPart, diffPart := splitCommitAndDiff(log)
	commitPart = filterCommitMessages(commitPart, excludeSet)
	diffPart = filterDiffEntries(diffPart, excludeSet)
	commitPart = truncateRunes(strings.TrimSpace(commitPart), maxTotalRunes/2)
	remaining := maxTotalRunes - len([]rune(commitPart))
	if remaining <= 0 || strings.TrimSpace(diffPart) == "" {
		return strings.TrimSpace(commitPart)
	}
	summary := summarizeDiff(diffPart, 10, 3)
	summary = truncateRunes(summary, remaining/3)
	remaining -= len([]rune(summary))
	if remaining <= 0 {
		return strings.TrimSpace(commitPart + "\n\nDiff Summary:\n" + summary)
	}
	truncated := truncateRunes(diffPart, remaining)
	var sb strings.Builder
	sb.WriteString(commitPart)
	if summary != "" {
		sb.WriteString("\n\nDiff Summary:\n")
		sb.WriteString(summary)
	}
	sb.WriteString("\n\nDiff (truncated):\n")
	sb.WriteString(truncated)
	return strings.TrimSpace(sb.String())
}

func splitCommitAndDiff(log string) (string, string) {
	marker := "\n\nDiff "
	idx := strings.Index(log, marker)
	if idx == -1 {
		return log, ""
	}
	return log[:idx], log[idx+2:]
}

func truncateRunes(text string, maxRunes int) string {
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return strings.TrimSpace(text)
	}
	trimmed := string(runes[:maxRunes])
	if idx := strings.LastIndex(trimmed, "\n"); idx > 0 {
		trimmed = trimmed[:idx]
	}
	return strings.TrimSpace(trimmed) + "\n…"
}

type diffSummary struct {
	Path      string
	Additions int
	Deletions int
	Samples   []string
}

func (t *Task) excludedPaths() []string {
	outputPath := coalesce(t.cfg.Apply.OutputPath, "./CHANGELOG.md")
	if strings.TrimSpace(outputPath) == "" {
		outputPath = "CHANGELOG.md"
	}
	if filepath.IsAbs(outputPath) {
		if rel, err := filepath.Rel(t.root, outputPath); err == nil {
			outputPath = rel
		}
	}
	outputPath = filepath.Clean(outputPath)
	outputPath = strings.TrimPrefix(outputPath, "./")
	outputPath = filepath.ToSlash(outputPath)
	if outputPath == "" {
		outputPath = "CHANGELOG.md"
	}
	return []string{strings.ToLower(outputPath), "changelog.md"}
}

func buildExcludeSet(paths []string) map[string]struct{} {
	set := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		trimmed := strings.ToLower(strings.TrimSpace(filepath.ToSlash(p)))
		set[trimmed] = struct{}{}
	}
	return set
}

func filterCommitMessages(commitPart string, exclude map[string]struct{}) string {
	lines := strings.Split(commitPart, "\n")
	var filtered []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		skip := strings.Contains(lower, "changelog")
		if !skip {
			for path := range exclude {
				if strings.Contains(lower, path) {
					skip = true
					break
				}
			}
		}
		if skip {
			continue
		}
		filtered = append(filtered, trimmed)
	}
	if len(filtered) == 0 {
		return strings.TrimSpace(commitPart)
	}
	return strings.Join(filtered, "\n")
}

func filterDiffEntries(diff string, exclude map[string]struct{}) string {
	scanner := bufio.NewScanner(strings.NewReader(diff))
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	var sb strings.Builder
	include := true
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "diff --git ") {
			path := strings.ToLower(strings.TrimSpace(filepath.ToSlash(extractPath(line))))
			_, skip := exclude[path]
			include = !skip
		}
		if include {
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}
	if err := scanner.Err(); err != nil {
		return diff
	}
	return strings.TrimRight(sb.String(), "\n")
}

func summarizeDiff(diff string, maxFiles int, maxSamples int) string {
	scanner := bufio.NewScanner(strings.NewReader(diff))
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	var summaries []diffSummary
	var current *diffSummary
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "diff --git ") {
			path := extractPath(line)
			entry := diffSummary{Path: path}
			summaries = append(summaries, entry)
			current = &summaries[len(summaries)-1]
			continue
		}
		if current == nil {
			continue
		}
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}
		if strings.HasPrefix(line, "@@") {
			continue
		}
		if strings.HasPrefix(line, "+") {
			current.Additions++
			if len(current.Samples) < maxSamples {
				current.Samples = append(current.Samples, strings.TrimSpace(line))
			}
			continue
		}
		if strings.HasPrefix(line, "-") {
			current.Deletions++
			if len(current.Samples) < maxSamples {
				current.Samples = append(current.Samples, strings.TrimSpace(line))
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Sprintf("Failed to summarize diff: %v", err)
	}
	if len(summaries) == 0 {
		return "No diff available."
	}
	originalCount := len(summaries)
	if maxFiles > 0 && len(summaries) > maxFiles {
		summaries = summaries[:maxFiles]
	}
	var sb strings.Builder
	for _, entry := range summaries {
		sb.WriteString(fmt.Sprintf("- %s (Δ+%d/-%d)\n", entry.Path, entry.Additions, entry.Deletions))
		for _, sample := range entry.Samples {
			sb.WriteString("    ")
			sb.WriteString(sample)
			sb.WriteString("\n")
		}
	}
	if maxFiles > 0 && originalCount > len(summaries) {
		sb.WriteString(fmt.Sprintf("- … %d additional files\n", originalCount-len(summaries)))
	}
	return strings.TrimRight(sb.String(), "\n")
}

func extractPath(line string) string {
	parts := strings.Fields(line)
	if len(parts) < 4 {
		return line
	}
	left := strings.TrimPrefix(parts[2], "a/")
	right := strings.TrimPrefix(parts[3], "b/")
	if strings.TrimSpace(right) != "" {
		return right
	}
	return left
}

func max1(a, b int) int {
	if a > 0 {
		return a
	}
	return b
}

// failTask surfaces configuration load errors through the normal Runner flow.
type failTask struct{ err error }

func (f failTask) Name() string { return "changelog" }
func (f failTask) Gather(ctx context.Context) (pipeline.GatherOutput, error) {
	return nil, f.err
}
func (f failTask) Prompt(ctx context.Context, _ pipeline.GatherOutput) (pipeline.LLMRequest, error) {
	return pipeline.LLMRequest{}, f.err
}
func (f failTask) Verify(ctx context.Context, _ pipeline.GatherOutput, _ pipeline.LLMResponse) (bool, pipeline.VerifiedOutput, *pipeline.RefineRequest, error) {
	return false, nil, nil, f.err
}
func (f failTask) Apply(ctx context.Context, _ pipeline.VerifiedOutput) (pipeline.ApplyReport, error) {
	return pipeline.ApplyReport{}, f.err
}

// NewFromConfig constructs the task directly from an already-parsed config.
func NewFromConfig(cfg Config) *Task {
	return &Task{cfg: cfg}
}
