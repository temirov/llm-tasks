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
	defaultTaskName:     buildSortPipeline,
	changelogRecipeName: buildChangelogPipeline,
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
	var changelogInputs changelogExecutionInputs
	var changelogCleanup func()
	recipeKey := strings.ToLower(strings.TrimSpace(targetRecipe.Name))
	if recipeKey == changelogRecipeName {
		changelogConfig, mapErr := config.MapChangelog(targetRecipe)
		if mapErr != nil {
			return fmt.Errorf("map changelog recipe %s: %w", targetRecipe.Name, mapErr)
		}
		var prepareErr error
		changelogInputs, prepareErr = prepareChangelogInputs(command.Context(), options, changelogConfig)
		if prepareErr != nil {
			return prepareErr
		}
		cleanup, injectErr := injectChangelogContext(changelogInputs.GitContext)
		if injectErr != nil {
			return injectErr
		}
		changelogCleanup = cleanup
		if options.dryRunSet && options.dryRun {
			changelogConfig.Apply.Mode = "print"
		}
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

	effectiveAttempts := resolveEffectiveAttempts(command, options, rootConfiguration)

	effectiveTimeout := time.Duration(rootConfiguration.Common.Defaults.TimeoutSeconds) * time.Second
	if options.timeout > 0 {
		effectiveTimeout = options.timeout
	}
	if effectiveTimeout <= 0 {
		effectiveTimeout = 45 * time.Second
	}

	effectiveDryRun := false
	if options.dryRunSet {
		effectiveDryRun = options.dryRun
	}

	runner := pipeline.Runner{
		Client: adapter,
		Options: pipeline.RunOptions{
			MaxAttempts: effectiveAttempts,
			DryRun:      effectiveDryRun,
			Timeout:     effectiveTimeout,
		},
	}

	taskPipeline, builderErr := buildPipeline(rootConfiguration, targetRecipe, mappedChangelogConfig)
	if builderErr != nil {
		return builderErr
	}

	executionContext := command.Context()
	if chTask, ok := taskPipeline.(*changelogtask.Task); ok {
		chTask.SetInputs(changelogInputs.Values)
	}
	if sortTask, ok := taskPipeline.(*sorttask.Task); ok {
		sourceOverride := strings.TrimSpace(options.sortSource)
		destinationOverride := strings.TrimSpace(options.sortDestination)
		if err := sortTask.SetBaseDirectories(sourceOverride, destinationOverride); err != nil {
			return err
		}
		if options.dryRunSet {
			sortTask.SetDryRunOverride(options.dryRun)
		}
		report, batchedErr := sorttask.RunBatches(executionContext, runner, sortTask, sorttask.DefaultBatchSize)
		if batchedErr != nil {
			return fmt.Errorf("run pipeline %s: %w", targetRecipe.Name, batchedErr)
		}
		if _, writeErr := fmt.Fprintf(command.OutOrStdout(), "%s (actions=%d, dry=%v)\n", report.Summary, report.NumActions, report.DryRun); writeErr != nil {
			return fmt.Errorf("write run result: %w", writeErr)
		}
		return nil
	}
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
	if mappedChangelogConfig != nil && strings.EqualFold(recipe.Name, changelogRecipeName) {
		return changelogtask.NewFromConfig(changelogtask.Config(*mappedChangelogConfig)), nil
	}

	builderKey := strings.ToLower(strings.TrimSpace(recipe.Name))
	builder, ok := pipelineBuilders[builderKey]
	if !ok {
		return nil, fmt.Errorf("unknown recipe %s", recipe.Name)
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
	Values     map[string]string
	GitContext string
}

func prepareChangelogInputs(ctx context.Context, options runCommandOptions, cfg config.ChangelogConfig) (changelogExecutionInputs, error) {
	definitionByName := map[string]config.InputDefinition{}
	for _, def := range cfg.Inputs {
		definitionByName[strings.ToLower(def.Name)] = def
	}

	versionFlag := strings.TrimSpace(options.changelogVersion)
	dateFlag := strings.TrimSpace(options.changelogDate)

	versionDef, hasVersion := definitionByName["version"]
	dateDef, hasDate := definitionByName["date"]
	gitLogDef, hasGitLog := definitionByName["git_log"]
	if !hasVersion || !hasDate || !hasGitLog {
		return changelogExecutionInputs{}, fmt.Errorf("changelog recipe must define inputs for version, date, and git_log")
	}
	for _, conflict := range versionDef.ConflictsWith {
		if conflict == "date" && versionFlag != "" && dateFlag != "" {
			return changelogExecutionInputs{}, fmt.Errorf(changelogMutuallyExclusiveFlagsErrorMessage)
		}
	}
	for _, conflict := range dateDef.ConflictsWith {
		if conflict == "version" && versionFlag != "" && dateFlag != "" {
			return changelogExecutionInputs{}, fmt.Errorf(changelogMutuallyExclusiveFlagsErrorMessage)
		}
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

	values := make(map[string]string)

	releaseVersion := strings.TrimSpace(versionFlag)
	if releaseVersion == "" {
		releaseVersion = strings.TrimSpace(versionDef.Default)
	}
	if releaseVersion == "" {
		releaseVersion = deriveNextVersion(strings.TrimSpace(result.BaseRef))
	}
	if releaseVersion == "" {
		releaseVersion = changelogDefaultVersionLabel
	}
	values["version"] = releaseVersion

	releaseDate := strings.TrimSpace(dateFlag)
	if releaseDate == "" {
		releaseDate = strings.TrimSpace(dateDef.Default)
	}
	if releaseDate == "" {
		releaseDate = time.Now().UTC().Format(time.DateOnly)
	}
	if dateDef.Type == "date" && releaseDate != "" {
		normalizedDate, err := normalizeDateInput(releaseDate)
		if err != nil {
			return changelogExecutionInputs{}, err
		}
		releaseDate = normalizedDate
	}
	values["date"] = releaseDate

	if strings.EqualFold(gitLogDef.Source, "stdin") {
		values["git_log"] = ""
	}

	return changelogExecutionInputs{
		Values:     values,
		GitContext: result.Context,
	}, nil
}

func normalizeDateInput(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if _, err := time.Parse(time.DateOnly, value); err == nil {
		return value, nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC().Format(time.DateOnly), nil
	}
	return "", fmt.Errorf("invalid date value: %s", value)
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
