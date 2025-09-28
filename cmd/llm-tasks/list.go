package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/temirov/llm-tasks/internal/config"
)

var (
	listAllFlag    bool
	rootConfigPath string
)

func init() {
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List recipes from config.yaml (enabled by default)",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := config.LoadRoot(rootConfigPath)
			if err != nil {
				return err
			}
			for _, r := range root.Recipes {
				if !listAllFlag && !r.Enabled {
					continue
				}
				state := "enabled"
				if !r.Enabled {
					state = "disabled"
				}
				// Write to Cobra's writer so tests can capture output reliably.
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t(%s, model=%s)\n", r.Name, state, dashIfEmpty(r.Model))
			}
			return nil
		},
	}

	listCmd.Flags().BoolVar(&listAllFlag, "all", false, "Show disabled recipes as well")
	listCmd.Flags().StringVar(&rootConfigPath, "config", "./config.yaml", "Path to unified config.yaml")

	rootCmd.AddCommand(listCmd)
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
