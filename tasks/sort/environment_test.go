package sort

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temirov/llm-tasks/internal/config"
)

func TestResolveSortGrantBaseDirectories(t *testing.T) {
	tempDownloads := t.TempDir()
	tempDestination := t.TempDir()

	source := config.Sort{}
	source.Grant.BaseDirectories.Downloads = tempDownloads
	source.Grant.BaseDirectories.Staging = tempDestination

	resolved, err := resolveSortGrantBaseDirectories(source)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.Grant.BaseDirectories.Downloads != filepath.Clean(tempDownloads) {
		t.Fatalf("expected downloads %s, got %s", filepath.Clean(tempDownloads), resolved.Grant.BaseDirectories.Downloads)
	}
	if resolved.Grant.BaseDirectories.Staging != filepath.Clean(tempDestination) {
		t.Fatalf("expected staging %s, got %s", filepath.Clean(tempDestination), resolved.Grant.BaseDirectories.Staging)
	}
}

func TestResolveSortGrantBaseDirectoriesExpandsTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("home directory unavailable")
	}

	source := config.Sort{}
	source.Grant.BaseDirectories.Downloads = filepath.Join("~", "Downloads")
	source.Grant.BaseDirectories.Staging = filepath.Join(home, "Sorted")

	resolved, err := resolveSortGrantBaseDirectories(source)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedDownloads := filepath.Join(home, "Downloads")
	if resolved.Grant.BaseDirectories.Downloads != expectedDownloads {
		t.Fatalf("expected downloads %s, got %s", expectedDownloads, resolved.Grant.BaseDirectories.Downloads)
	}
}

func TestResolveSortGrantBaseDirectoriesRejectsEnvReferences(t *testing.T) {
	source := config.Sort{}
	source.Grant.BaseDirectories.Downloads = "${SORT_DOWNLOADS_DIR}"
	source.Grant.BaseDirectories.Staging = "/tmp/staging"

	_, err := resolveSortGrantBaseDirectories(source)
	expected := fmt.Sprintf(sortGrantDirectoryEnvUnsupportedFormat, sortGrantDownloadsDirectoryKey)
	if err == nil || !strings.Contains(err.Error(), expected) {
		t.Fatalf("expected env reference error, got %v", err)
	}
}

func TestResolveSortGrantBaseDirectoriesRejectsBlank(t *testing.T) {
	source := config.Sort{}
	source.Grant.BaseDirectories.Downloads = "  "
	source.Grant.BaseDirectories.Staging = "/tmp/staging"

	_, err := resolveSortGrantBaseDirectories(source)
	if err == nil || !strings.Contains(err.Error(), sortGrantDownloadsDirectoryKey) {
		t.Fatalf("expected blank error, got %v", err)
	}
}

func TestValidateBaseDirectoriesRejectsIdentical(t *testing.T) {
	base := t.TempDir()
	if err := validateBaseDirectories(base, base); err == nil {
		t.Fatalf("expected error when directories are identical")
	}
}
