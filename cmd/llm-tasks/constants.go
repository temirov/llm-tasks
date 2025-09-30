package llmtasks

const (
	defaultConfigPath                = "./config.yaml"
	defaultTaskName                  = "sort"
	runCommandUse                    = "run [RECIPE]"
	runCommandShort                  = "Run a registered LLM task (pipeline)"
	runCommandArgsMin                = 0
	runCommandArgsMax                = 1
	configFlagName                   = "config"
	configFlagUsage                  = "Path to unified config.yaml"
	allFlagName                      = "all"
	allFlagUsage                     = "Show disabled recipes as well"
	taskNameFlagName                 = "name"
	taskNameFlagUsage                = "Recipe name to run (from config.yaml)"
	attemptsFlagName                 = "attempts"
	attemptsFlagUsage                = "Max refine attempts (0 = use defaults)"
	timeoutFlagName                  = "timeout"
	timeoutFlagUsage                 = "Per-attempt timeout (e.g., 45s; 0 = use defaults)"
	modelFlagName                    = "model"
	modelFlagUsage                   = "Override recipe's model by name (must exist in models[])"
	versionFlagName                  = "version"
	versionFlagUsage                 = "Changelog version to inject into the gather step"
	dateFlagName                     = "date"
	dateFlagUsage                    = "Changelog release date to inject into the gather step"
	listCommandUse                   = "list"
	listCommandShort                 = "List recipes from config.yaml (enabled by default)"
	enabledStateLabel                = "enabled"
	disabledStateLabel               = "disabled"
	dashPlaceholder                  = "-"
	defaultAPIEndpoint               = "https://api.openai.com/v1"
	defaultAPIKeyEnvironmentVariable = "OPENAI_API_KEY"
)
