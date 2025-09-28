// cmd/llm-tasks/task_run.go
package main

import (
	"context"
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

var (
	runTaskName      string
	runAttempts      int
	runTimeout       time.Duration
	runModelOverride string
)

// runTask is reused by the root-level `run` command (see run.go).
func runTask(cmd *cobra.Command, args []string) error {
	root, err := config.LoadRoot(runConfigPath)
	if err != nil {
		return err
	}

	// Resolve recipe by name
	rx, ok := root.FindRecipe(runTaskName)
	if !ok || !rx.Enabled {
		return fmt.Errorf("unknown or disabled recipe %q", runTaskName)
	}

	// Resolve model: override -> recipe.model -> default model
	modelName := strings.TrimSpace(runModelOverride)
	if modelName == "" {
		modelName = strings.TrimSpace(rx.Model)
	}
	if modelName == "" {
		if def, ok := root.DefaultModel(); ok {
			modelName = def.Name
		}
	}
	mc, ok := root.FindModel(modelName)
	if !ok {
		return fmt.Errorf("model %q not found in models[]", modelName)
	}

	// Build low-level HTTP client
	apiKeyEnv := root.Common.API.APIKeyEnv
	if strings.TrimSpace(apiKeyEnv) == "" {
		apiKeyEnv = "OPENAI_API_KEY"
	}
	apiKey := strings.TrimSpace(os.Getenv(apiKeyEnv))
	if apiKey == "" {
		return fmt.Errorf("missing API key: set %s", apiKeyEnv)
	}
	endpoint := root.Common.API.Endpoint
	if strings.TrimSpace(endpoint) == "" {
		endpoint = "https://api.openai.com/v1"
	}

	httpClient := llm.Client{
		HTTPBaseURL:       endpoint,
		APIKey:            apiKey,
		ModelIdentifier:   mc.ModelID,
		MaxTokensResponse: mc.MaxCompletionTokens,
		Temperature:       mc.DefaultTemperature,
	}
	adapter := llm.Adapter{
		Client:              httpClient,
		DefaultModel:        mc.ModelID,
		DefaultTemp:         mc.DefaultTemperature,
		DefaultTokens:       mc.MaxCompletionTokens,
		SupportsTemperature: mc.SupportsTemperature,
	}

	// Runner options
	attempts := root.Common.Defaults.Attempts
	if runAttempts > 0 {
		attempts = runAttempts
	}
	if attempts <= 0 {
		attempts = 3
	}
	timeout := time.Duration(root.Common.Defaults.TimeoutSeconds) * time.Second
	if runTimeout > 0 {
		timeout = runTimeout
	}
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	runner := pipeline.Runner{
		Client:  adapter,
		Options: pipeline.RunOptions{MaxAttempts: attempts, DryRun: false, Timeout: timeout},
	}

	// Registry and task construction strictly from recipe type
	var task pipeline.Pipeline
	switch rx.Type {
	case "task/sort":
		task = sorttask.NewWithDeps(
			sorttask.DefaultFS(), // OS-backed fsops
			sorttask.NewUnifiedProvider(root, rx.Name),
		)
	case "task/changelog":
		cfg, mapErr := config.MapChangelog(rx)
		if mapErr != nil {
			return mapErr
		}
		task = changelogtask.NewFromConfig(changelogtask.Config(cfg))
	default:
		return fmt.Errorf("unknown recipe type: %s", rx.Type)
	}

	ctx := context.Background()
	report, err := runner.Run(ctx, task)
	if err != nil {
		return err
	}
	fmt.Printf("%s (actions=%d, dry=%v)\n", report.Summary, report.NumActions, report.DryRun)
	return nil
}
