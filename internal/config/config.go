package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Root struct {
	Common  Common   `yaml:"common"`
	Models  []Model  `yaml:"models"`
	Recipes []Recipe `yaml:"recipes"`
}

type Common struct {
	API struct {
		Endpoint  string `yaml:"endpoint"`
		APIKeyEnv string `yaml:"api_key_env"`
	} `yaml:"api"`
	Logging struct {
		Level  string `yaml:"level"`
		Format string `yaml:"format"`
	} `yaml:"logging"`
	Defaults struct {
		Attempts       int `yaml:"attempts"`
		TimeoutSeconds int `yaml:"timeout_seconds"`
	} `yaml:"defaults"`
}

type Model struct {
	Name                string  `yaml:"name"`
	Provider            string  `yaml:"provider"`
	ModelID             string  `yaml:"model_id"`
	Default             bool    `yaml:"default"`
	SupportsTemperature bool    `yaml:"supports_temperature"`
	DefaultTemperature  float64 `yaml:"default_temperature"`
	MaxCompletionTokens int     `yaml:"max_completion_tokens"`
}

type Recipe struct {
	Name    string `yaml:"name"`
	Enabled bool   `yaml:"enabled"`
	Model   string `yaml:"model"` // may be empty -> default model
	Type    string `yaml:"type"`  // e.g., task/sort, task/changelog

	// Inline capture of the rest of fields. We re-marshal into task-specific config.
	Body map[string]any `yaml:",inline"`
}

// ------------------ Load & helpers ------------------

func LoadRoot(path string) (Root, error) {
	var out Root
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return Root{}, err
	}
	if err := yaml.Unmarshal(data, &out); err != nil {
		return Root{}, err
	}
	// Basic validation
	if len(out.Models) == 0 {
		return Root{}, errors.New("config.models is empty")
	}
	if _, ok := out.DefaultModel(); !ok {
		return Root{}, errors.New("no default model found (set models[].default: true)")
	}
	return out, nil
}

func (r Root) DefaultModel() (Model, bool) {
	for _, m := range r.Models {
		if m.Default {
			return m, true
		}
	}
	return Model{}, false
}

func (r Root) FindModel(name string) (Model, bool) {
	for _, m := range r.Models {
		if m.Name == name {
			return m, true
		}
	}
	return Model{}, false
}

func (r Root) FindRecipe(name string) (Recipe, bool) {
	for _, x := range r.Recipes {
		if x.Name == name {
			return x, true
		}
	}
	return Recipe{}, false
}

// ------------------ Sort recipe mapping ------------------

type SortYAML struct {
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

// New provider data for tasks/sort.
func MapSort(rx Recipe) (SortYAML, error) {
	var out SortYAML
	b, err := yaml.Marshal(rx.Body)
	if err != nil {
		return out, err
	}
	if err := yaml.Unmarshal(b, &out); err != nil {
		return out, fmt.Errorf("map sort recipe: %w", err)
	}
	return out, nil
}

// ------------------ Changelog recipe mapping ------------------

type ChangelogConfig struct {
	Task string `yaml:"task"` // fixed by task constructor
	LLM  struct {
		Model       string  `yaml:"model"`
		Temperature float64 `yaml:"temperature"`
		MaxTokens   int     `yaml:"max_tokens"`
	} `yaml:"llm"`
	Inputs struct {
		Version struct {
			Required bool   `yaml:"required"`
			Env      string `yaml:"env"`
			Default  string `yaml:"default"`
		} `yaml:"version"`
		Date struct {
			Required bool   `yaml:"required"`
			Env      string `yaml:"env"`
			Default  string `yaml:"default"`
		} `yaml:"date"`
		GitLog struct {
			Required bool   `yaml:"required"`
			Source   string `yaml:"source"`
		} `yaml:"git_log"`
	} `yaml:"inputs"`
	Recipe struct {
		System string `yaml:"system"`
		Format struct {
			Heading  string `yaml:"heading"`
			Sections []struct {
				Title string `yaml:"title"`
				Min   int    `yaml:"min"`
				Max   int    `yaml:"max"`
			} `yaml:"sections"`
			Footer string `yaml:"footer"`
		} `yaml:"format"`
		Rules []string `yaml:"rules"`
	} `yaml:"recipe"`
	Apply struct {
		OutputPath      string `yaml:"output_path"`
		Mode            string `yaml:"mode"`
		EnsureBlankLine bool   `yaml:"ensure_blank_line"`
	} `yaml:"apply"`
}

func MapChangelog(rx Recipe) (ChangelogConfig, error) {
	var out ChangelogConfig
	b, err := yaml.Marshal(rx.Body)
	if err != nil {
		return out, err
	}
	if err := yaml.Unmarshal(b, &out); err != nil {
		return out, fmt.Errorf("map changelog recipe: %w", err)
	}
	// Fill minimal task+llm defaults; the runner sets real model & temperature via adapter.
	out.Task = "changelog"
	if out.LLM.MaxTokens <= 0 {
		out.LLM.MaxTokens = 1200
	}
	return out, nil
}

// ------------------ Legacy Sort struct + loader (used by tasks/sort) ------------------

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

// LoadSort keeps compatibility with the file-based provider used by tasks/sort.
func LoadSort(path string) (Sort, error) {
	var out Sort
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return Sort{}, err
	}
	if err := yaml.Unmarshal(data, &out); err != nil {
		return Sort{}, err
	}
	return out, nil
}
