package main

import "github.com/spf13/cobra"

// Keep `task` as a parent command. Subcommands are defined in task_run.go and task_list.go.
func init() { rootCmd.AddCommand(taskCmd) }

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Task commands (see subcommands)",
}
