package llmtasks

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/temirov/llm-tasks/internal/config"
)

const (
	sortSourceFlagName       = "source"
	sortDestinationFlagName  = "destination"
	sortSourceFlagUsage      = "Source directory containing files to classify"
	sortDestinationFlagUsage = "Destination directory where classified files will be placed"
)

type runCommandOptions struct {
	configPath       string
	taskName         string
	attempts         int
	timeout          time.Duration
	modelOverride    string
	changelogVersion string
	changelogDate    string
	changelogRoot    string
	sortSource       string
	sortDestination  string
	dryRun           bool
	dryRunSet        bool
}

func newRunCommand() *cobra.Command {
	options := &runCommandOptions{
		configPath: defaultConfigPath,
		taskName:   defaultTaskName,
	}

	command := &cobra.Command{
		Use:   runCommandUse,
		Short: runCommandShort,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 2 {
				dryRunFlag := cmd.Flags().Lookup(dryRunFlagName)
				if dryRunFlag != nil && dryRunFlag.Changed {
					if _, ok := parseBoolChoice(args[1]); ok {
						return nil
					}
					return fmt.Errorf("invalid boolean value %q for --%s", args[1], dryRunFlagName)
				}
			}
			return cobra.RangeArgs(runCommandArgsMin, runCommandArgsMax)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			effectiveOptions := *options
			dryRunFlag := cmd.Flags().Lookup(dryRunFlagName)
			dryRunChanged := dryRunFlag != nil && dryRunFlag.Changed
			effectiveArgs, dryRunOverride := splitDryRunArgument(args, dryRunChanged)
			if len(effectiveArgs) > 0 {
				effectiveOptions.taskName = effectiveArgs[0]
			}
			if dryRunOverride != nil {
				effectiveOptions.dryRun = *dryRunOverride
				effectiveOptions.dryRunSet = true
			} else {
				effectiveOptions.dryRunSet = dryRunChanged
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
	command.Flags().StringVar(&options.changelogRoot, changelogRootFlagName, "", changelogRootFlagUsage)
	command.Flags().StringVar(&options.sortSource, sortSourceFlagName, "", sortSourceFlagUsage)
	command.Flags().StringVar(&options.sortDestination, sortDestinationFlagName, "", sortDestinationFlagUsage)
	dryRunValue := newBoolChoiceValue(&options.dryRun)
	command.Flags().Var(dryRunValue, dryRunFlagName, dryRunFlagUsage)
	if dryRunFlag := command.Flags().Lookup(dryRunFlagName); dryRunFlag != nil {
		dryRunFlag.NoOptDefVal = "true"
		dryRunFlag.DefValue = "false"
	}

	defaultHelpFunc := command.HelpFunc()
	command.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		recipe := resolveTargetRecipe(cmd, options, args)
		withRecipeVisibility(cmd, options, recipe, func() {
			defaultHelpFunc(cmd, args)
		})
	})
	defaultUsageFunc := command.UsageFunc()
	command.SetUsageFunc(func(cmd *cobra.Command) error {
		recipe := resolveTargetRecipe(cmd, options, nil)
		var usageErr error
		withRecipeVisibility(cmd, options, recipe, func() {
			usageErr = defaultUsageFunc(cmd)
		})
		return usageErr
	})

	return command
}

type environmentFlag struct {
	FlagName string
	EnvName  string
	Required bool
	Value    *string
	Recipe   string
}

func detectConfigPath(args []string) string {
	for index := 0; index < len(args); index++ {
		arg := strings.TrimSpace(args[index])
		if arg == "--"+configFlagName && index+1 < len(args) {
			return strings.TrimSpace(args[index+1])
		}
		if strings.HasPrefix(arg, "--"+configFlagName+"=") {
			return strings.TrimSpace(strings.TrimPrefix(arg, "--"+configFlagName+"="))
		}
	}
	return defaultConfigPath
}

func captureHidden(flag *pflag.Flag) func() {
	original := flag.Hidden
	return func() { flag.Hidden = original }
}

func resolveTargetRecipe(cmd *cobra.Command, options *runCommandOptions, args []string) string {
	targetRecipeName := strings.TrimSpace(options.taskName)
	nonFlagArguments := cmd.Flags().Args()
	if len(nonFlagArguments) > 0 {
		targetRecipeName = strings.TrimSpace(nonFlagArguments[0])
	} else if len(args) > 0 {
		targetRecipeName = strings.TrimSpace(args[0])
	} else if taskNameFlag := cmd.Flags().Lookup(taskNameFlagName); taskNameFlag != nil {
		targetRecipeName = strings.TrimSpace(taskNameFlag.Value.String())
	}
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
	return targetRecipeName
}

func withRecipeVisibility(cmd *cobra.Command, options *runCommandOptions, recipe string, fn func()) {
	var restore []func()

	if versionFlag := cmd.Flags().Lookup(changelogVersionFlagName); versionFlag != nil {
		restore = append(restore, captureHidden(versionFlag))
		versionFlag.Hidden = !strings.EqualFold(recipe, changelogRecipeName)
		if strings.EqualFold(recipe, changelogRecipeName) && !strings.Contains(versionFlag.Usage, changelogVersionRequiredSuffix) {
			versionFlag.Usage = strings.TrimSpace(versionFlag.Usage + " " + changelogVersionRequiredSuffix)
		}
	}
	if dateFlag := cmd.Flags().Lookup(changelogDateFlagName); dateFlag != nil {
		restore = append(restore, captureHidden(dateFlag))
		dateFlag.Hidden = !strings.EqualFold(recipe, changelogRecipeName)
	}
	if rootFlag := cmd.Flags().Lookup(changelogRootFlagName); rootFlag != nil {
		restore = append(restore, captureHidden(rootFlag))
		rootFlag.Hidden = !strings.EqualFold(recipe, changelogRecipeName)
	}
	isSort := strings.EqualFold(recipe, defaultTaskName)
	if sourceFlag := cmd.Flags().Lookup(sortSourceFlagName); sourceFlag != nil {
		restore = append(restore, captureHidden(sourceFlag))
		sourceFlag.Hidden = !isSort
	}
	if destinationFlag := cmd.Flags().Lookup(sortDestinationFlagName); destinationFlag != nil {
		restore = append(restore, captureHidden(destinationFlag))
		destinationFlag.Hidden = !isSort
	}

	originalUse := cmd.Use
	trimmedRecipe := strings.TrimSpace(recipe)
	if trimmedRecipe != "" {
		cmd.Use = fmt.Sprintf("run %s", trimmedRecipe)
	} else {
		cmd.Use = runCommandUse
	}
	restore = append(restore, func() { cmd.Use = originalUse })

	fn()

	for i := len(restore) - 1; i >= 0; i-- {
		restore[i]()
	}
}

func resolveEffectiveAttempts(cmd *cobra.Command, options runCommandOptions, root config.Root) int {
	attemptFlag := cmd.Flags().Lookup(attemptsFlagName)
	if attemptFlag != nil && attemptFlag.Changed {
		if options.attempts <= 0 {
			return 0
		}
		return options.attempts
	}
	effective := root.Common.Defaults.Attempts
	if effective < 0 {
		effective = 0
	}
	return effective
}

func splitDryRunArgument(args []string, dryRunFlagChanged bool) ([]string, *bool) {
	trimmed := make([]string, len(args))
	copy(trimmed, args)
	if !dryRunFlagChanged || len(args) == 0 {
		return trimmed, nil
	}
	candidate := args[len(args)-1]
	if boolValue, ok := parseBoolChoice(candidate); ok {
		trimmed = trimmed[:len(trimmed)-1]
		return trimmed, &boolValue
	}
	return trimmed, nil
}
