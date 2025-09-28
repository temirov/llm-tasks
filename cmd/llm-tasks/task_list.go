package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/temirov/llm-tasks/internal/pipeline"
	changelogtask "github.com/temirov/llm-tasks/tasks/changelog"
	sorttask "github.com/temirov/llm-tasks/tasks/sort"
)

func init() {
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List available tasks",
		Run: func(cmd *cobra.Command, args []string) {
			reg := pipeline.NewRegistry()
			reg.Register("sort", func() pipeline.Pipeline { return sorttask.New() })
			// YAML-only changelog task
			reg.Register("changelog", func() pipeline.Pipeline { return changelogtask.New() })
			for _, n := range reg.Names() {
				fmt.Println(n)
			}
		},
	}
	taskCmd.AddCommand(listCmd)
}
