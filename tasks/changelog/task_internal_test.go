package changelog

import (
	"strings"
	"testing"
)

func TestNormalizeGitLogExcludesChangelog(t *testing.T) {
	log := "Commits v0.9.0..HEAD:\nabc123 chore: update changelog formatting\ndef456 feat: awesome feature\n\nDiff v0.9.0..HEAD:\ndiff --git a/CHANGELOG.md b/CHANGELOG.md\nindex 111..222 100644\n--- a/CHANGELOG.md\n+++ b/CHANGELOG.md\n+added\n\ndiff --git a/src/main.go b/src/main.go\nindex 333..444 100644\n--- a/src/main.go\n+++ b/src/main.go\n+feature code\n"
	res := normalizeGitLog(log, 2000, []string{"CHANGELOG.md"})
	lower := strings.ToLower(res)
	if strings.Contains(lower, "changelog.md") {
		t.Fatalf("expected changelog diff to be excluded, got %s", res)
	}
	if strings.Contains(lower, "update changelog") {
		t.Fatalf("expected changelog commit to be filtered, got %s", res)
	}
	if !strings.Contains(lower, "awesome feature") {
		t.Fatalf("expected other commits to remain, got %s", res)
	}
}
