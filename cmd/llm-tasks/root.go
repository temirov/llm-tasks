package llmtasks

import "github.com/spf13/cobra"

const (
	rootUse   = "llm-tasks"
	rootShort = "CLI to run LLM tasks"
)

// NewRootCommand builds the root command for the llm-tasks CLI.
func NewRootCommand() *cobra.Command {
	rootCommand := &cobra.Command{
		Use:   rootUse,
		Short: rootShort,
	}

	rootCommand.AddCommand(newListCommand())
	rootCommand.AddCommand(newRunCommand())

	return rootCommand
}

// Execute runs the llm-tasks CLI.
func Execute() error {
	return NewRootCommand().Execute()
}
