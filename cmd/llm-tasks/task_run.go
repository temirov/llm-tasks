package llmtasks

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/temirov/llm-tasks/internal/config"
	"github.com/temirov/llm-tasks/internal/llm"
	"github.com/temirov/llm-tasks/internal/pipeline"
	changelogtask "github.com/temirov/llm-tasks/tasks/changelog"
	sorttask "github.com/temirov/llm-tasks/tasks/sort"
)

type pipelineBuilder func(root config.Root, recipe config.Recipe) (pipeline.Pipeline, error)

const (
	sortRecipeType      = "task/sort"
	changelogRecipeType = "task/changelog"
)

var pipelineBuilders = map[string]pipelineBuilder{
	sortRecipeType: buildSortPipeline,
}

func runTaskCommand(command *cobra.Command, options runCommandOptions) error {
	rootConfiguration, err := config.LoadRoot(options.configPath)
	if err != nil {
		return fmt.Errorf("load root configuration %s: %w", options.configPath, err)
	}

	targetRecipe, recipeFound := rootConfiguration.FindRecipe(options.taskName)
	if !recipeFound || !targetRecipe.Enabled {
		return fmt.Errorf("unknown or disabled recipe %q", options.taskName)
	}

	selectedModelName := resolveModelName(options, targetRecipe, rootConfiguration)
	modelConfiguration, modelFound := rootConfiguration.FindModel(selectedModelName)
	if !modelFound {
		return fmt.Errorf("model %q not found in models[]", selectedModelName)
	}

	apiKeyEnvironmentVariable := strings.TrimSpace(rootConfiguration.Common.API.APIKeyEnv)
	if apiKeyEnvironmentVariable == "" {
		apiKeyEnvironmentVariable = defaultAPIKeyEnvironmentVariable
	}
	apiKey := strings.TrimSpace(os.Getenv(apiKeyEnvironmentVariable))
	if apiKey == "" {
		return fmt.Errorf("missing API key: set %s", apiKeyEnvironmentVariable)
	}

	apiEndpoint := strings.TrimSpace(rootConfiguration.Common.API.Endpoint)
	if apiEndpoint == "" {
		apiEndpoint = defaultAPIEndpoint
	}

	httpClient := llm.Client{
		HTTPBaseURL:       apiEndpoint,
		APIKey:            apiKey,
		ModelIdentifier:   modelConfiguration.ModelID,
		MaxTokensResponse: modelConfiguration.MaxCompletionTokens,
		Temperature:       modelConfiguration.DefaultTemperature,
	}
	adapter := llm.Adapter{
		Client:              httpClient,
		DefaultModel:        modelConfiguration.ModelID,
		DefaultTemp:         modelConfiguration.DefaultTemperature,
		DefaultTokens:       modelConfiguration.MaxCompletionTokens,
		SupportsTemperature: modelConfiguration.SupportsTemperature,
	}

	effectiveAttempts := rootConfiguration.Common.Defaults.Attempts
	if options.attempts > 0 {
		effectiveAttempts = options.attempts
	}
	if effectiveAttempts <= 0 {
		effectiveAttempts = 3
	}

	effectiveTimeout := time.Duration(rootConfiguration.Common.Defaults.TimeoutSeconds) * time.Second
	if options.timeout > 0 {
		effectiveTimeout = options.timeout
	}
	if effectiveTimeout <= 0 {
		effectiveTimeout = 45 * time.Second
	}

	runner := pipeline.Runner{
		Client: adapter,
		Options: pipeline.RunOptions{
			MaxAttempts: effectiveAttempts,
			DryRun:      false,
			Timeout:     effectiveTimeout,
		},
	}

	taskPipeline, builderErr := buildPipeline(rootConfiguration, targetRecipe, options)
	if builderErr != nil {
		return builderErr
	}

	executionContext := command.Context()
	report, runErr := runner.Run(executionContext, taskPipeline)
	if runErr != nil {
		return fmt.Errorf("run pipeline %s: %w", targetRecipe.Name, runErr)
	}

	_, writeErr := fmt.Fprintf(command.OutOrStdout(), "%s (actions=%d, dry=%v)\n", report.Summary, report.NumActions, report.DryRun)
	if writeErr != nil {
		return fmt.Errorf("write run result: %w", writeErr)
	}

	return nil
}

func resolveModelName(options runCommandOptions, recipe config.Recipe, root config.Root) string {
	modelName := strings.TrimSpace(options.modelOverride)
	if modelName != "" {
		return modelName
	}

	recipeModel := strings.TrimSpace(recipe.Model)
	if recipeModel != "" {
		return recipeModel
	}

	defaultModel, ok := root.DefaultModel()
	if ok {
		return defaultModel.Name
	}

	return ""
}

func buildPipeline(root config.Root, recipe config.Recipe, options runCommandOptions) (pipeline.Pipeline, error) {
	if recipe.Type == changelogRecipeType {
		mappedConfig, err := config.MapChangelog(recipe)
		if err != nil {
			return nil, fmt.Errorf("map changelog recipe %s: %w", recipe.Name, err)
		}

		if trimmedVersion := strings.TrimSpace(options.version); trimmedVersion != "" {
			mappedConfig.Inputs.Version.Default = trimmedVersion
		}
		if trimmedDate := strings.TrimSpace(options.releaseDate); trimmedDate != "" {
			mappedConfig.Inputs.Date.Default = trimmedDate
		}

		return changelogtask.NewFromConfig(mappedConfig), nil
	}

	builder, ok := pipelineBuilders[recipe.Type]
	if !ok {
		return nil, fmt.Errorf("unknown recipe type: %s", recipe.Type)
	}

	pipelineInstance, err := builder(root, recipe)
	if err != nil {
		return nil, fmt.Errorf("build pipeline for recipe %s: %w", recipe.Name, err)
	}

	return pipelineInstance, nil
}

func buildSortPipeline(root config.Root, recipe config.Recipe) (pipeline.Pipeline, error) {
	provider := sorttask.NewUnifiedProvider(root, recipe.Name)
	return sorttask.NewWithDeps(sorttask.DefaultFS(), provider), nil
}
