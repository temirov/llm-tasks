package llmtasks

import (
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type runCommandOptions struct {
	configPath    string
	taskName      string
	attempts      int
	timeout       time.Duration
	modelOverride string
	version       string
	releaseDate   string
}

func newRunCommand() *cobra.Command {
	options := &runCommandOptions{
		configPath: defaultConfigPath,
		taskName:   defaultTaskName,
	}

	command := &cobra.Command{
		Use:   runCommandUse,
		Short: runCommandShort,
		Args:  cobra.RangeArgs(runCommandArgsMin, runCommandArgsMax),
		RunE: func(cmd *cobra.Command, args []string) error {
			effectiveOptions := *options
			if len(args) > 0 {
				effectiveOptions.taskName = args[0]
			}
			return runTaskCommand(cmd, effectiveOptions)
		},
	}

	command.Flags().StringVar(&options.taskName, taskNameFlagName, defaultTaskName, taskNameFlagUsage)
	command.Flags().IntVar(&options.attempts, attemptsFlagName, 0, attemptsFlagUsage)
	command.Flags().DurationVar(&options.timeout, timeoutFlagName, 0, timeoutFlagUsage)
	command.Flags().StringVar(&options.modelOverride, modelFlagName, "", modelFlagUsage)
	command.Flags().StringVar(&options.configPath, configFlagName, defaultConfigPath, configFlagUsage)
	command.Flags().Var(newTrimmedStringValue(&options.version), versionFlagName, versionFlagUsage)
	command.Flags().Var(newTrimmedStringValue(&options.releaseDate), dateFlagName, dateFlagUsage)

	return command
}

type trimmedStringValue struct {
	target *string
}

func newTrimmedStringValue(target *string) *trimmedStringValue {
	return &trimmedStringValue{target: target}
}

func (value *trimmedStringValue) Set(input string) error {
	if value.target == nil {
		return nil
	}
	trimmed := strings.TrimSpace(input)
	*value.target = trimmed
	return nil
}

func (value *trimmedStringValue) String() string {
	if value == nil || value.target == nil {
		return ""
	}
	return *value.target
}

func (value *trimmedStringValue) Type() string {
	return "string"
}
