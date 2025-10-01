package llmtasks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/temirov/llm-tasks/internal/config"
	"github.com/temirov/llm-tasks/internal/gitcontext"
	"github.com/temirov/llm-tasks/internal/llm"
	"github.com/temirov/llm-tasks/internal/pipeline"
	changelogtask "github.com/temirov/llm-tasks/tasks/changelog"
	sorttask "github.com/temirov/llm-tasks/tasks/sort"
)

type pipelineBuilder func(root config.Root, recipe config.Recipe) (pipeline.Pipeline, error)

var pipelineBuilders = map[string]pipelineBuilder{
	sortRecipeType:      buildSortPipeline,
	changelogRecipeType: buildChangelogPipeline,
}

func runTaskCommand(command *cobra.Command, options runCommandOptions) error {
	rootConfiguration, err := loadRootConfiguration(options.configPath)
	if err != nil {
		return err
	}

	targetRecipe, recipeFound := rootConfiguration.FindRecipe(options.taskName)
	if !recipeFound || !targetRecipe.Enabled {
		return fmt.Errorf("unknown or disabled recipe %q", options.taskName)
	}

	var mappedChangelogConfig *config.ChangelogConfig
	var changelogCleanup func()
	if targetRecipe.Type == changelogRecipeType {
		changelogConfig, mapErr := config.MapChangelog(targetRecipe)
		if mapErr != nil {
			return fmt.Errorf("map changelog recipe %s: %w", targetRecipe.Name, mapErr)
		}
		inputs, prepareErr := prepareChangelogInputs(command.Context(), options, changelogConfig)
		if prepareErr != nil {
			return prepareErr
		}
		if err := applyChangelogEnvironment(changelogConfig, inputs); err != nil {
			return err
		}
		cleanup, injectErr := injectChangelogContext(inputs.GitContext)
		if injectErr != nil {
			return injectErr
		}
		changelogCleanup = cleanup
		mappedChangelogConfig = &changelogConfig
	}
	defer func() {
		if changelogCleanup != nil {
			changelogCleanup()
		}
	}()

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

	taskPipeline, builderErr := buildPipeline(rootConfiguration, targetRecipe, mappedChangelogConfig)
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

func buildPipeline(root config.Root, recipe config.Recipe, mappedChangelogConfig *config.ChangelogConfig) (pipeline.Pipeline, error) {
	if mappedChangelogConfig != nil && recipe.Type == changelogRecipeType {
		return changelogtask.NewFromConfig(changelogtask.Config(*mappedChangelogConfig)), nil
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

func buildChangelogPipeline(root config.Root, recipe config.Recipe) (pipeline.Pipeline, error) {
	mappedConfig, err := config.MapChangelog(recipe)
	if err != nil {
		return nil, fmt.Errorf("map changelog recipe %s: %w", recipe.Name, err)
	}
	return changelogtask.NewFromConfig(changelogtask.Config(mappedConfig)), nil
}

type changelogExecutionInputs struct {
	Version    string
	Date       string
	GitContext string
}

func prepareChangelogInputs(ctx context.Context, options runCommandOptions, cfg config.ChangelogConfig) (changelogExecutionInputs, error) {
	versionFlag := strings.TrimSpace(options.changelogVersion)
	dateFlag := strings.TrimSpace(options.changelogDate)
	if versionFlag != "" && dateFlag != "" {
		return changelogExecutionInputs{}, fmt.Errorf(changelogMutuallyExclusiveFlagsErrorMessage)
	}

	collector := gitcontext.NewCollector()
	result, err := collector.Collect(ctx, gitcontext.Options{
		ExplicitVersion: versionFlag,
		ExplicitDate:    dateFlag,
	})
	if err != nil {
		if errors.Is(err, gitcontext.ErrDateAndVersionProvided) {
			return changelogExecutionInputs{}, fmt.Errorf(changelogMutuallyExclusiveFlagsErrorMessage)
		}
		if errors.Is(err, gitcontext.ErrStartingPointUnavailable) {
			return changelogExecutionInputs{}, fmt.Errorf(changelogStartingPointRequiredErrorMessage)
		}
		if errors.Is(err, gitcontext.ErrNoCommitsInRange) {
			return changelogExecutionInputs{}, fmt.Errorf(changelogNoCommitsErrorFormat, err)
		}
		return changelogExecutionInputs{}, fmt.Errorf(changelogContextCollectionErrorFormat, err)
	}

	releaseVersion := strings.TrimSpace(versionFlag)
	if releaseVersion == "" {
		releaseVersion = strings.TrimSpace(cfg.Inputs.Version.Default)
	}
	if releaseVersion == "" {
		derived := deriveNextVersion(strings.TrimSpace(result.BaseRef))
		if derived != "" {
			releaseVersion = derived
		}
	}
	if releaseVersion == "" {
		releaseVersion = changelogDefaultVersionLabel
	}

	releaseDate := dateFlag
	if releaseDate == "" {
		releaseDate = strings.TrimSpace(cfg.Inputs.Date.Default)
	}
	if releaseDate == "" {
		releaseDate = time.Now().UTC().Format(time.DateOnly)
	}

	return changelogExecutionInputs{
		Version:    releaseVersion,
		Date:       releaseDate,
		GitContext: result.Context,
	}, nil
}

func applyChangelogEnvironment(cfg config.ChangelogConfig, inputs changelogExecutionInputs) error {
	versionEnv := strings.TrimSpace(cfg.Inputs.Version.Env)
	if versionEnv != "" {
		if setErr := os.Setenv(versionEnv, inputs.Version); setErr != nil {
			return fmt.Errorf(setEnvironmentVariableErrorFormat, versionEnv, setErr)
		}
	}
	dateEnv := strings.TrimSpace(cfg.Inputs.Date.Env)
	if dateEnv != "" {
		if setErr := os.Setenv(dateEnv, inputs.Date); setErr != nil {
			return fmt.Errorf(setEnvironmentVariableErrorFormat, dateEnv, setErr)
		}
	}
	return nil
}

func injectChangelogContext(contextPayload string) (func(), error) {
	reader, writer, pipeErr := os.Pipe()
	if pipeErr != nil {
		return nil, fmt.Errorf(changelogContextPipeErrorFormat, pipeErr)
	}
	go func() {
		_, _ = io.WriteString(writer, contextPayload)
		_ = writer.Close()
	}()
	originalStdin := os.Stdin
	os.Stdin = reader
	return func() {
		_ = reader.Close()
		os.Stdin = originalStdin
	}, nil
}

func deriveNextVersion(baseRef string) string {
	trimmed := strings.TrimSpace(baseRef)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(trimmed, "v") {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(trimmed, "v"), ".")
	if len(parts) != 3 {
		return ""
	}
	major, errMajor := strconv.Atoi(parts[0])
	minor, errMinor := strconv.Atoi(parts[1])
	patch, errPatch := strconv.Atoi(parts[2])
	if errMajor != nil || errMinor != nil || errPatch != nil {
		return ""
	}
	return fmt.Sprintf("v%d.%d.%d", major, minor, patch+1)
}
