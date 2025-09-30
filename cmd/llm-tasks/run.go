package llmtasks

import (
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type runCommandOptions struct {
	configPath       string
	taskName         string
	attempts         int
	timeout          time.Duration
	modelOverride    string
	changelogVersion string
	changelogDate    string
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
	command.Flags().StringVar(&options.changelogVersion, changelogVersionFlagName, "", changelogVersionFlagUsage)
	command.Flags().StringVar(&options.changelogDate, changelogDateFlagName, "", changelogDateFlagUsage)

	defaultHelpFunc := command.HelpFunc()
	command.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		targetRecipeName := strings.TrimSpace(options.taskName)
		nonFlagArguments := cmd.Flags().Args()
		if len(nonFlagArguments) > 0 {
			targetRecipeName = strings.TrimSpace(nonFlagArguments[0])
		} else if len(args) > 0 {
			targetRecipeName = strings.TrimSpace(args[0])
		} else if taskNameFlag := cmd.Flags().Lookup(taskNameFlagName); taskNameFlag != nil {
			targetRecipeName = strings.TrimSpace(taskNameFlag.Value.String())
		}

		if !strings.EqualFold(targetRecipeName, changelogRecipeName) {
			taskNamePrefix := "--" + taskNameFlagName
			for index := 0; index < len(args); index++ {
				trimmedArgument := strings.TrimSpace(args[index])
				if strings.HasPrefix(trimmedArgument, taskNamePrefix+"=") {
					targetRecipeName = strings.TrimSpace(strings.TrimPrefix(trimmedArgument, taskNamePrefix+"="))
					break
				}
				if trimmedArgument == taskNamePrefix && index+1 < len(args) {
					targetRecipeName = strings.TrimSpace(args[index+1])
					break
				}
			}
		}

		if strings.EqualFold(targetRecipeName, changelogRecipeName) {
			versionFlag := cmd.Flags().Lookup(changelogVersionFlagName)
			if versionFlag != nil && !strings.Contains(versionFlag.Usage, changelogVersionRequiredSuffix) {
				versionFlag.Usage = strings.TrimSpace(versionFlag.Usage + " " + changelogVersionRequiredSuffix)
			}
		}

		defaultHelpFunc(cmd, args)
	})

	return command
}
