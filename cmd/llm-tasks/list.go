package llmtasks

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/temirov/llm-tasks/internal/config"
)

type listCommandOptions struct {
	includeDisabled bool
	configPath      string
}

func newListCommand() *cobra.Command {
	options := &listCommandOptions{configPath: defaultConfigPath}

	command := &cobra.Command{
		Use:   listCommandUse,
		Short: listCommandShort,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runListCommand(cmd, *options)
		},
	}

	command.Flags().BoolVar(&options.includeDisabled, allFlagName, false, allFlagUsage)
	command.Flags().StringVar(&options.configPath, configFlagName, defaultConfigPath, configFlagUsage)

	return command
}

func runListCommand(command *cobra.Command, options listCommandOptions) error {
	rootConfiguration, err := config.LoadRoot(options.configPath)
	if err != nil {
		return fmt.Errorf("load root configuration %s: %w", options.configPath, err)
	}

	for _, recipe := range rootConfiguration.Recipes {
		if !options.includeDisabled && !recipe.Enabled {
			continue
		}

		recipeStateLabel := enabledStateLabel
		if !recipe.Enabled {
			recipeStateLabel = disabledStateLabel
		}

		outputWriter := command.OutOrStdout()
		_, writeErr := fmt.Fprintf(outputWriter, "%s\t(%s, model=%s)\n", recipe.Name, recipeStateLabel, dashIfEmpty(recipe.Model))
		if writeErr != nil {
			return fmt.Errorf("write recipe listing: %w", writeErr)
		}
	}

	return nil
}

func dashIfEmpty(value string) string {
	if value == "" {
		return dashPlaceholder
	}
	return value
}
