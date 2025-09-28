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
	runTaskName   string
	runAttempts   int
	runTimeout    time.Duration
	runDry        bool
	runModel      string
	runEndpoint   string
	runAPIKey     string
	runSortConfig string

	// YAML-only changelog flags
	runChangelogConfig string
	runVersion         string
	runDate            string

	appCfg       config.App
	appCfgLoaded bool
)

func init() {
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run a registered LLM task (pipeline)",
		RunE:  runTask,
	}

	runCmd.Flags().StringVar(&runTaskName, "name", "sort", "Task name to run (e.g., 'sort')")
	runCmd.Flags().IntVar(&runAttempts, "attempts", 0, "Max refine attempts (0 = use defaults)")
	runCmd.Flags().DurationVar(&runTimeout, "timeout", 0, "Per-attempt timeout (e.g., 45s; 0 = use defaults)")
	runCmd.Flags().BoolVar(&runDry, "dry", false, "Dry-run (for tasks that support it)")
	runCmd.Flags().StringVar(&runModel, "model", "", "Model override (default from app config)")
	runCmd.Flags().StringVar(&runEndpoint, "endpoint", "", "API endpoint override (default from app config)")
	runCmd.Flags().StringVar(&runAPIKey, "api-key", "", "API key override (default read from env in app config)")
	runCmd.Flags().StringVar(&runSortConfig, "sort-config", "", "Path to sort task yaml (if set, exported to LLMTASKS_SORT_CONFIG)")

	// Changelog (YAML-only) unified flow
	runCmd.Flags().StringVar(&runChangelogConfig, "changelog-config", "", "Path to changelog task yaml (exported to LLMTASKS_CHANGELOG_CONFIG)")
	runCmd.Flags().StringVar(&runVersion, "version", "", "Version for changelog (exported to CHANGELOG_VERSION)")
	runCmd.Flags().StringVar(&runDate, "date", "", "Date for changelog (exported to CHANGELOG_DATE)")

	taskCmd.AddCommand(runCmd)
}

func runTask(cmd *cobra.Command, args []string) error {
	// Route per-task configs/env via process env so tasks can read them uniformly.

	// sort config
	if runSortConfig != "" {
		if err := os.Setenv("LLMTASKS_SORT_CONFIG", runSortConfig); err != nil {
			return err
		}
	}

	// changelog config + inputs (YAML-only)
	if runChangelogConfig != "" {
		if err := os.Setenv("LLMTASKS_CHANGELOG_CONFIG", runChangelogConfig); err != nil {
			return err
		}
	}
	if runVersion != "" {
		if err := os.Setenv("CHANGELOG_VERSION", runVersion); err != nil {
			return err
		}
	}
	if runDate != "" {
		if err := os.Setenv("CHANGELOG_DATE", runDate); err != nil {
			return err
		}
	}

	// Load app config (defaults to ./configs/app.yaml)
	if err := mustLoadAppCfg(); err != nil {
		return err
	}

	// Register tasks
	reg := pipeline.NewRegistry()
	reg.Register("sort", func() pipeline.Pipeline { return sorttask.New() })
	// YAML-only changelog task factory reads LLMTASKS_CHANGELOG_CONFIG or defaults
	reg.Register("changelog", func() pipeline.Pipeline { return changelogtask.New() })

	task, ok := reg.Create(runTaskName)
	if !ok {
		return fmt.Errorf("unknown task %q", runTaskName)
	}

	// Build LLM HTTP client + adapter
	endpoint := firstNonEmpty(runEndpoint, appCfg.API.Endpoint, "https://api.openai.com/v1")
	apiKey, err := getAPIKey(runAPIKey)
	if err != nil {
		return err
	}
	model := firstNonEmpty(runModel, appCfg.Defaults.Model, "gpt-5-mini")

	maxTokens := appCfg.Defaults.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1500
	}
	temp := appCfg.Defaults.Temperature
	if temp <= 0 {
		temp = 0.2
	}

	httpClient := llm.Client{
		HTTPBaseURL:       endpoint,
		APIKey:            apiKey,
		ModelIdentifier:   model,
		MaxTokensResponse: maxTokens,
		Temperature:       temp,
	}
	adapter := llm.Adapter{
		Client:        httpClient,
		DefaultModel:  model,
		DefaultTemp:   temp,
		DefaultTokens: maxTokens,
	}

	// Runner options
	attempts := appCfg.Defaults.Attempts
	if runAttempts > 0 {
		attempts = runAttempts
	}
	if attempts <= 0 {
		attempts = 3
	}
	timeout := time.Duration(appCfg.Defaults.TimeoutSeconds) * time.Second
	if runTimeout > 0 {
		timeout = runTimeout
	}
	if timeout <= 0 {
		timeout = 45 * time.Second
	}

	runner := pipeline.Runner{
		Client:  adapter,
		Options: pipeline.RunOptions{MaxAttempts: attempts, DryRun: runDry, Timeout: timeout},
	}

	ctx := context.Background()
	report, err := runner.Run(ctx, task)
	if err != nil {
		return err
	}

	fmt.Printf("%s (actions=%d, dry=%v)\n", report.Summary, report.NumActions, report.DryRun)
	return nil
}

// ---- config helpers ----

func mustLoadAppCfg() error {
	if appCfgLoaded {
		return nil
	}
	path := firstNonEmpty(os.Getenv("LLMTASKS_APP_CONFIG"), "configs/app.yaml")
	cfg, err := config.LoadApp(path)
	if err != nil {
		// Fall back to minimal defaults if the file isn’t present.
		appCfg.API.Endpoint = firstNonEmpty(os.Getenv("LLMTASKS_API_ENDPOINT"), "https://api.openai.com/v1")
		appCfg.API.APIKeyEnv = firstNonEmpty(os.Getenv("LLMTASKS_API_KEY_ENV"), "OPENAI_API_KEY")
		appCfg.Defaults.Model = "gpt-5-mini"
		appCfg.Defaults.Temperature = 0.2
		appCfg.Defaults.MaxTokens = 1500
		appCfg.Defaults.Attempts = 3
		appCfg.Defaults.TimeoutSeconds = 45
		appCfgLoaded = true
		return nil
	}
	appCfg = cfg
	appCfgLoaded = true
	return nil
}

func getAPIKey(flagVal string) (string, error) {
	if strings.TrimSpace(flagVal) != "" {
		return flagVal, nil
	}
	if !appCfgLoaded {
		if err := mustLoadAppCfg(); err != nil {
			return "", err
		}
	}
	envName := firstNonEmpty(appCfg.API.APIKeyEnv, "OPENAI_API_KEY")
	if v := strings.TrimSpace(os.Getenv(envName)); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("missing API key: pass --api-key or set %s", envName)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
