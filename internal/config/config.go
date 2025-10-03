package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	changelogTaskName                        = "changelog"
	emptyModelsErrorMessage                  = "config.models is empty"
	missingDefaultModelErrorMessage          = "no default model found (set models[].default: true)"
	rootConfigurationEmptyContentErrorFormat = "root configuration %s is empty"
	rootConfigurationUnmarshalErrorFormat    = "unmarshal root configuration %s: %w"
	mapSortMarshalErrorFormat                = "marshal sort recipe: %w"
	mapSortUnmarshalErrorFormat              = "map sort recipe: %w"
	mapChangelogUnmarshalErrorFormat         = "map changelog recipe: %w"
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
	Model   string `yaml:"model"`

	Body map[string]any `yaml:",inline"`
}

type InputDefinition struct {
	Name          string   `yaml:"name"`
	Description   string   `yaml:"description,omitempty"`
	Required      bool     `yaml:"required"`
	Type          string   `yaml:"type"`
	Source        string   `yaml:"source,omitempty"`
	Default       string   `yaml:"default,omitempty"`
	ConflictsWith []string `yaml:"conflicts_with,omitempty"`
}

// LoadRoot parses the provided configuration source and validates required fields.
func LoadRoot(source RootConfigurationSource) (Root, error) {
	if len(source.Content) == 0 {
		return Root{}, fmt.Errorf(rootConfigurationEmptyContentErrorFormat, source.Reference)
	}

	var rootConfiguration Root
	if err := yaml.Unmarshal(source.Content, &rootConfiguration); err != nil {
		return Root{}, fmt.Errorf(rootConfigurationUnmarshalErrorFormat, source.Reference, err)
	}

	if len(rootConfiguration.Models) == 0 {
		return Root{}, errors.New(emptyModelsErrorMessage)
	}
	if _, ok := rootConfiguration.DefaultModel(); !ok {
		return Root{}, errors.New(missingDefaultModelErrorMessage)
	}
	return rootConfiguration, nil
}

func (root Root) DefaultModel() (Model, bool) {
	for _, modelConfiguration := range root.Models {
		if modelConfiguration.Default {
			return modelConfiguration, true
		}
	}
	return Model{}, false
}

func (root Root) FindModel(name string) (Model, bool) {
	for _, modelConfiguration := range root.Models {
		if modelConfiguration.Name == name {
			return modelConfiguration, true
		}
	}
	return Model{}, false
}

func (root Root) FindRecipe(name string) (Recipe, bool) {
	for _, recipe := range root.Recipes {
		if recipe.Name == name {
			return recipe, true
		}
	}
	return Recipe{}, false
}

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

// MapSort converts a recipe into the SortYAML structure expected by the sort task.
func MapSort(recipe Recipe) (SortYAML, error) {
	var sortConfiguration SortYAML
	encodedRecipeBody, marshalError := yaml.Marshal(recipe.Body)
	if marshalError != nil {
		return sortConfiguration, fmt.Errorf(mapSortMarshalErrorFormat, marshalError)
	}
	if err := yaml.Unmarshal(encodedRecipeBody, &sortConfiguration); err != nil {
		return sortConfiguration, fmt.Errorf(mapSortUnmarshalErrorFormat, err)
	}
	return sortConfiguration, nil
}

type ChangelogConfig struct {
	Task string `yaml:"task"`
	LLM  struct {
		Model       string  `yaml:"model"`
		Temperature float64 `yaml:"temperature"`
		MaxTokens   int     `yaml:"max_tokens"`
	} `yaml:"llm"`
	Inputs []InputDefinition `yaml:"inputs"`
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

// MapChangelog converts a recipe into the changelog task configuration schema.
func MapChangelog(recipe Recipe) (ChangelogConfig, error) {
	var changelogConfiguration ChangelogConfig
	encodedRecipeBody, marshalError := yaml.Marshal(recipe.Body)
	if marshalError != nil {
		return changelogConfiguration, marshalError
	}
	if err := yaml.Unmarshal(encodedRecipeBody, &changelogConfiguration); err != nil {
		return changelogConfiguration, fmt.Errorf(mapChangelogUnmarshalErrorFormat, err)
	}
	changelogConfiguration.Task = changelogTaskName
	if changelogConfiguration.LLM.MaxTokens <= 0 {
		changelogConfiguration.LLM.MaxTokens = 1200
	}
	inputNames := make(map[string]struct{})
	for index := range changelogConfiguration.Inputs {
		def := &changelogConfiguration.Inputs[index]
		def.Name = strings.TrimSpace(def.Name)
		if def.Name == "" {
			return ChangelogConfig{}, fmt.Errorf("changelog inputs[%d]: name is required", index)
		}
		if _, exists := inputNames[def.Name]; exists {
			return ChangelogConfig{}, fmt.Errorf("duplicate changelog input name %q", def.Name)
		}
		inputNames[def.Name] = struct{}{}
		def.Type = strings.TrimSpace(strings.ToLower(def.Type))
		if def.Type == "" {
			return ChangelogConfig{}, fmt.Errorf("changelog inputs[%s]: type is required", def.Name)
		}
		def.Source = strings.TrimSpace(strings.ToLower(def.Source))
		if def.Source == "" {
			def.Source = "flag"
		}
		if len(def.ConflictsWith) > 0 {
			normalized := make([]string, 0, len(def.ConflictsWith))
			for _, conflict := range def.ConflictsWith {
				trimmed := strings.TrimSpace(strings.ToLower(conflict))
				if trimmed == "" {
					continue
				}
				normalized = append(normalized, trimmed)
			}
			def.ConflictsWith = normalized
		}
	}
	return changelogConfiguration, nil
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

// LoadSort reads a legacy sort configuration file from disk.
func LoadSort(path string) (Sort, error) {
	var sortConfiguration Sort
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return Sort{}, err
	}
	if err := yaml.Unmarshal(data, &sortConfiguration); err != nil {
		return Sort{}, err
	}
	return sortConfiguration, nil
}
