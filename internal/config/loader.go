package config

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	embeddedRootConfigurationReference = "embedded default configuration"
	// EmbeddedRootConfigurationReference identifies the embedded fallback configuration source.
	EmbeddedRootConfigurationReference          = embeddedRootConfigurationReference
	explicitConfigurationReadErrorFormat        = "read explicit configuration %s: %w"
	loaderInitializationWorkingDirectoryError   = "determine working directory: %w"
	loaderHomeEnvironmentVariableName           = "HOME"
	workingDirectoryConfigurationFileName       = "config.yaml"
	homeDirectoryConfigurationRelativeDirectory = ".llm-tasks"
	homeDirectoryConfigurationFileName          = "config.yaml"
)

var (
	//go:embed default_root_configuration.yaml
	embeddedRootConfigurationBytes []byte
)

// RootConfigurationSource holds the raw configuration data and its origin.
type RootConfigurationSource struct {
	Reference string
	Content   []byte
}

// RootConfigurationLoader locates configuration files across supported search paths.
type RootConfigurationLoader struct {
	workingDirectory string
	homeDirectory    string
	fileReader       func(string) ([]byte, error)
}

// NewRootConfigurationLoader constructs a loader with the provided directories.
func NewRootConfigurationLoader(workingDirectory string, homeDirectory string) RootConfigurationLoader {
	return RootConfigurationLoader{
		workingDirectory: workingDirectory,
		homeDirectory:    homeDirectory,
		fileReader:       os.ReadFile,
	}
}

// NewDefaultRootConfigurationLoader builds a loader using the process working directory and HOME.
func NewDefaultRootConfigurationLoader() (RootConfigurationLoader, error) {
	workingDirectory, workingDirectoryError := os.Getwd()
	if workingDirectoryError != nil {
		return RootConfigurationLoader{}, fmt.Errorf(loaderInitializationWorkingDirectoryError, workingDirectoryError)
	}
	homeDirectory := os.Getenv(loaderHomeEnvironmentVariableName)
	return NewRootConfigurationLoader(workingDirectory, homeDirectory), nil
}

type configurationCandidate struct {
	path       string
	isExplicit bool
}

// Load resolves the configuration source using the preferred search order.
func (loader RootConfigurationLoader) Load(explicitPath string) (RootConfigurationSource, error) {
	configurationCandidates := loader.candidates(explicitPath)
	for _, candidate := range configurationCandidates {
		if candidate.path == "" {
			continue
		}
		content, readError := loader.fileReader(candidate.path)
		if readError != nil {
			if candidate.isExplicit && !errors.Is(readError, fs.ErrNotExist) && !errors.Is(readError, fs.ErrPermission) {
				return RootConfigurationSource{}, fmt.Errorf(explicitConfigurationReadErrorFormat, candidate.path, readError)
			}
			continue
		}
		return RootConfigurationSource{Reference: candidate.path, Content: content}, nil
	}
	return RootConfigurationSource{Reference: embeddedRootConfigurationReference, Content: embeddedRootConfigurationBytes}, nil
}

func (loader RootConfigurationLoader) candidates(explicitPath string) []configurationCandidate {
	homeDirectoryCandidate := loader.homeDirectoryCandidate()
	workingDirectoryCandidate := loader.workingDirectoryCandidate()
	explicitCandidate := configurationCandidate{path: explicitPath, isExplicit: explicitPath != ""}
	return []configurationCandidate{explicitCandidate, workingDirectoryCandidate, homeDirectoryCandidate}
}

func (loader RootConfigurationLoader) workingDirectoryCandidate() configurationCandidate {
	if loader.workingDirectory == "" {
		return configurationCandidate{}
	}
	workingDirectoryPath := filepath.Join(loader.workingDirectory, workingDirectoryConfigurationFileName)
	return configurationCandidate{path: workingDirectoryPath}
}

func (loader RootConfigurationLoader) homeDirectoryCandidate() configurationCandidate {
	if loader.homeDirectory == "" {
		return configurationCandidate{}
	}
	configurationDirectory := filepath.Join(loader.homeDirectory, homeDirectoryConfigurationRelativeDirectory)
	configurationPath := filepath.Join(configurationDirectory, homeDirectoryConfigurationFileName)
	return configurationCandidate{path: configurationPath}
}
