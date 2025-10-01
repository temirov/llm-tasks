package gitcontext_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/temirov/llm-tasks/internal/gitcontext"
)

func TestCollectorUsesLatestVersionTag(t *testing.T) {
	repositoryDir := t.TempDir()
	initializeGitRepository(t, repositoryDir)

	createFile(t, repositoryDir, "README.md", "initial")
	runGitCommand(t, repositoryDir, "add", "README.md")
	runGitCommand(t, repositoryDir, "commit", "-m", "initial commit")
	runGitCommand(t, repositoryDir, "tag", "v1.0.0")

	createFile(t, repositoryDir, "feature.txt", "feature work")
	runGitCommand(t, repositoryDir, "add", "feature.txt")
	runGitCommand(t, repositoryDir, "commit", "-m", "feat: add feature")

	collector := gitcontext.NewCollector()
	result, err := collector.Collect(context.Background(), gitcontext.Options{WorkingDir: repositoryDir})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	if !strings.Contains(result.CommitSummary, "feat: add feature") {
		t.Fatalf("expected commit summary to include latest commit: %s", result.CommitSummary)
	}
	if result.RangeDescription != "v1.0.0..HEAD" {
		t.Fatalf("expected range description to use latest tag, got %s", result.RangeDescription)
	}
	if result.BaseRef != "v1.0.0" {
		t.Fatalf("expected base ref to equal latest tag, got %s", result.BaseRef)
	}
	if !strings.Contains(result.Context, "Diff v1.0.0..HEAD:") {
		t.Fatalf("expected context to include diff header, got %s", result.Context)
	}
}

func TestCollectorRequiresStartingPointWhenNoTags(t *testing.T) {
	repositoryDir := t.TempDir()
	initializeGitRepository(t, repositoryDir)

	createFile(t, repositoryDir, "README.md", "initial")
	runGitCommand(t, repositoryDir, "add", "README.md")
	runGitCommand(t, repositoryDir, "commit", "-m", "initial commit")

	collector := gitcontext.NewCollector()
	_, err := collector.Collect(context.Background(), gitcontext.Options{WorkingDir: repositoryDir})
	if !errors.Is(err, gitcontext.ErrStartingPointUnavailable) {
		t.Fatalf("expected ErrStartingPointUnavailable, got %v", err)
	}
}

func TestCollectorErrorsWhenNoCommitsInRange(t *testing.T) {
	repositoryDir := t.TempDir()
	initializeGitRepository(t, repositoryDir)

	createFile(t, repositoryDir, "README.md", "initial")
	runGitCommand(t, repositoryDir, "add", "README.md")
	runGitCommand(t, repositoryDir, "commit", "-m", "initial commit")
	runGitCommand(t, repositoryDir, "tag", "v1.0.0")

	collector := gitcontext.NewCollector()
	_, err := collector.Collect(context.Background(), gitcontext.Options{WorkingDir: repositoryDir})
	if !errors.Is(err, gitcontext.ErrNoCommitsInRange) {
		t.Fatalf("expected ErrNoCommitsInRange, got %v", err)
	}
}

func TestCollectorSinceExplicitDateIgnoresTags(t *testing.T) {
	repositoryDir := t.TempDir()
	initializeGitRepository(t, repositoryDir)

	createFile(t, repositoryDir, "README.md", "initial")
	runGitCommand(t, repositoryDir, "add", "README.md")
	runGitCommand(t, repositoryDir, "commit", "-m", "initial commit")
	runGitCommand(t, repositoryDir, "tag", "v1.0.0")

	time.Sleep(2 * time.Second)
	sinceMarker := time.Now().UTC().Format(time.RFC3339)
	time.Sleep(2 * time.Second)

	createFile(t, repositoryDir, "second.txt", "second")
	runGitCommand(t, repositoryDir, "add", "second.txt")
	runGitCommand(t, repositoryDir, "commit", "-m", "second commit")

	collector := gitcontext.NewCollector()
	result, err := collector.Collect(context.Background(), gitcontext.Options{WorkingDir: repositoryDir, ExplicitDate: sinceMarker})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	if result.RangeDescription != "since "+sinceMarker {
		t.Fatalf("expected range description to honor date, got %s", result.RangeDescription)
	}
	if result.BaseRef != "" {
		t.Fatalf("expected base ref to be empty when using date, got %s", result.BaseRef)
	}
	if !strings.Contains(result.CommitSummary, "second commit") {
		t.Fatalf("expected commit summary to include commits after date, got %s", result.CommitSummary)
	}
	if strings.Contains(result.CommitSummary, "initial commit") {
		t.Fatalf("expected commit summary to exclude earlier commits, got %s", result.CommitSummary)
	}
}

func initializeGitRepository(t *testing.T, dir string) {
	t.Helper()
	runGitCommand(t, dir, "init")
	runGitCommand(t, dir, "config", "user.email", "ci@example.com")
	runGitCommand(t, dir, "config", "user.name", "CI User")
}

func createFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func runGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}
