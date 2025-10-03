package llmtasks

import (
	"fmt"

	"github.com/temirov/llm-tasks/internal/config"
)

func loadRootConfiguration(configurationPath string) (config.Root, error) {
	configurationLoader, loaderErr := config.NewDefaultRootConfigurationLoader()
	if loaderErr != nil {
		return config.Root{}, fmt.Errorf(configurationLoaderInitializationErrorFormat, loaderErr)
	}
	configurationSource, sourceErr := configurationLoader.Load(configurationPath)
	if sourceErr != nil {
		if configurationPath == "" || configurationPath == defaultConfigPath {
			configurationSource, sourceErr = configurationLoader.Load("")
		}
		if sourceErr != nil {
			return config.Root{}, fmt.Errorf(configurationSourceResolutionErrorFormat, sourceErr)
		}
	}
	rootConfiguration, loadErr := config.LoadRoot(configurationSource)
	if loadErr != nil {
		return config.Root{}, fmt.Errorf(rootConfigurationLoadErrorFormat, configurationSource.Reference, loadErr)
	}
	return rootConfiguration, nil
}
