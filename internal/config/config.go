package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type App struct {
	API struct {
		Endpoint  string `yaml:"endpoint"`
		APIKeyEnv string `yaml:"api_key_env"`
	} `yaml:"api"`
	Defaults struct {
		Model          string  `yaml:"model"`
		Temperature    float64 `yaml:"temperature"`
		MaxTokens      int     `yaml:"max_tokens"`
		Attempts       int     `yaml:"attempts"`
		TimeoutSeconds int     `yaml:"timeout_seconds"`
	} `yaml:"defaults"`
}

type Sort struct {
	Grant struct {
		BaseDirectories struct {
			Downloads string `yaml:"downloads"`
			Staging   string `yaml:"staging"`
		} `yaml:"base_directories"`
		Safety struct {
			DryRun bool `yaml:"dry_run"`
		} `yaml:"safety"`
	} `yaml:"grant"`
	Projects []struct {
		Name     string   `yaml:"name"`
		Target   string   `yaml:"target"`
		Keywords []string `yaml:"keywords"`
	} `yaml:"projects"`
	Thresholds struct {
		MinConfidence float64 `yaml:"min_confidence"`
	} `yaml:"thresholds"`
}

func LoadApp(path string) (App, error) {
	var out App
	if err := readYAML(path, &out); err != nil {
		return App{}, err
	}
	return out, nil
}

func LoadSort(path string) (Sort, error) {
	var out Sort
	if err := readYAML(path, &out); err != nil {
		return Sort{}, err
	}
	return out, nil
}

func readYAML(path string, dst any) error {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, dst)
}
