package main

import (
	"github.com/spf13/cobra"
)

var runConfigPath string

// Root-level `run` command so you can do: `llm-tasks run <recipe>`.
// It reuses the same run flags/logic from task_run.go.
func init() {
	rootRunCmd := &cobra.Command{
		Use:   "run [RECIPE]",
		Short: "Run a registered LLM task (pipeline)",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Allow positional recipe name to override --name
			if len(args) > 0 {
				runTaskName = args[0]
			}
			return runTask(cmd, args)
		},
	}

	// Flags (shared vars live in task_run.go)
	rootRunCmd.Flags().StringVar(&runTaskName, "name", "sort", "Recipe name to run (from config.yaml)")
	rootRunCmd.Flags().IntVar(&runAttempts, "attempts", 0, "Max refine attempts (0 = use defaults)")
	rootRunCmd.Flags().DurationVar(&runTimeout, "timeout", 0, "Per-attempt timeout (e.g., 45s; 0 = use defaults)")
	rootRunCmd.Flags().StringVar(&runModelOverride, "model", "", "Override recipe's model by name (must exist in models[])")

	// Config path at root level as well
	rootRunCmd.Flags().StringVar(&runConfigPath, "config", "./config.yaml", "Path to unified config.yaml")

	rootCmd.AddCommand(rootRunCmd)
}
