package sort

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/temirov/llm-tasks/internal/config"
)

const (
	sortGrantDownloadsDirectoryKey         = "grant.base_directories.downloads"
	sortGrantStagingDirectoryKey           = "grant.base_directories.staging"
	sortGrantDirectoryBlankErrorFormat     = "resolve %s: expanded value is blank"
	sortGrantDirectoryEnvUnsupportedFormat = "resolve %s: environment references are not supported"
)

func resolveSortGrantBaseDirectories(source config.Sort) (config.Sort, error) {
	resolved := source

	downloads, err := sanitizeBaseDirectory(source.Grant.BaseDirectories.Downloads, sortGrantDownloadsDirectoryKey)
	if err != nil {
		return config.Sort{}, err
	}
	staging, err := sanitizeBaseDirectory(source.Grant.BaseDirectories.Staging, sortGrantStagingDirectoryKey)
	if err != nil {
		return config.Sort{}, err
	}

	if err := validateBaseDirectories(downloads, staging); err != nil {
		return config.Sort{}, err
	}

	resolved.Grant.BaseDirectories.Downloads = downloads
	resolved.Grant.BaseDirectories.Staging = staging
	return resolved, nil
}

func sanitizeBaseDirectory(raw string, key string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf(sortGrantDirectoryBlankErrorFormat, key)
	}
	if strings.Contains(trimmed, "$") {
		return "", fmt.Errorf(sortGrantDirectoryEnvUnsupportedFormat, key)
	}
	normalized, err := normalizePath(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", key, err)
	}
	return normalized, nil
}

func normalizePath(pathValue string) (string, error) {
	trimmed := strings.TrimSpace(pathValue)
	if trimmed == "" {
		return "", fmt.Errorf("path is blank")
	}
	if strings.HasPrefix(trimmed, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		trimmed = filepath.Join(home, strings.TrimPrefix(trimmed, "~"))
	}
	if !filepath.IsAbs(trimmed) {
		abs, err := filepath.Abs(trimmed)
		if err != nil {
			return "", fmt.Errorf("resolve absolute path: %w", err)
		}
		trimmed = abs
	}
	return filepath.Clean(trimmed), nil
}

func validateBaseDirectories(sourceDir, destinationDir string) error {
	if strings.TrimSpace(sourceDir) == "" {
		return fmt.Errorf(sortGrantDirectoryBlankErrorFormat, sortGrantDownloadsDirectoryKey)
	}
	if strings.TrimSpace(destinationDir) == "" {
		return fmt.Errorf(sortGrantDirectoryBlankErrorFormat, sortGrantStagingDirectoryKey)
	}

	sourceAbs, err := normalizePath(sourceDir)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", sortGrantDownloadsDirectoryKey, err)
	}
	destinationAbs, err := normalizePath(destinationDir)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", sortGrantStagingDirectoryKey, err)
	}

	if sourceAbs == destinationAbs {
		return fmt.Errorf("source and destination directories must differ: %s", sourceAbs)
	}
	return nil
}
