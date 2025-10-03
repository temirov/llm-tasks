package llmtasks

import (
	"testing"

	"github.com/spf13/cobra"

	"github.com/temirov/llm-tasks/internal/config"
)

func TestResolveEffectiveAttempts(t *testing.T) {
	root := config.Root{}
	root.Common.Defaults.Attempts = 3

	defaultCmd := &cobra.Command{Use: "test"}
	temp := 0
	defaultCmd.Flags().IntVar(&temp, attemptsFlagName, 0, "")
	if got := resolveEffectiveAttempts(defaultCmd, runCommandOptions{}, root); got != 3 {
		t.Fatalf("expected default attempts 3, got %d", got)
	}

	flagCmd := &cobra.Command{Use: "test"}
	options := runCommandOptions{}
	flagCmd.Flags().IntVar(&options.attempts, attemptsFlagName, 0, "")
	if err := flagCmd.Flags().Set(attemptsFlagName, "0"); err != nil {
		t.Fatalf("set attempts flag: %v", err)
	}
	if got := resolveEffectiveAttempts(flagCmd, options, root); got != 0 {
		t.Fatalf("expected attempts 0 when flag set to 0, got %d", got)
	}

	flagCmdPositive := &cobra.Command{Use: "test"}
	optionsPositive := runCommandOptions{}
	flagCmdPositive.Flags().IntVar(&optionsPositive.attempts, attemptsFlagName, 0, "")
	if err := flagCmdPositive.Flags().Set(attemptsFlagName, "2"); err != nil {
		t.Fatalf("set attempts flag: %v", err)
	}
	if got := resolveEffectiveAttempts(flagCmdPositive, optionsPositive, root); got != 2 {
		t.Fatalf("expected attempts 2 when flag set to 2, got %d", got)
	}

	root.Common.Defaults.Attempts = -1
	negativeCmd := &cobra.Command{Use: "test"}
	temp = 0
	negativeCmd.Flags().IntVar(&temp, attemptsFlagName, 0, "")
	if got := resolveEffectiveAttempts(negativeCmd, runCommandOptions{}, root); got != 0 {
		t.Fatalf("expected attempts 0 when config default negative, got %d", got)
	}
}

func TestParseBoolChoice(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected bool
		ok       bool
	}{
		{name: "EmptyDefaultsTrue", input: "", expected: true, ok: true},
		{name: "TrueWord", input: "true", expected: true, ok: true},
		{name: "FalseWord", input: "false", expected: false, ok: true},
		{name: "Yes", input: "yes", expected: true, ok: true},
		{name: "No", input: "no", expected: false, ok: true},
		{name: "Upper", input: "ON", expected: true, ok: true},
		{name: "Invalid", input: "maybe", expected: false, ok: false},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(testingT *testing.T) {
			value, ok := parseBoolChoice(testCase.input)
			if ok != testCase.ok {
				testingT.Fatalf("expected ok=%v, got %v", testCase.ok, ok)
			}
			if ok && value != testCase.expected {
				testingT.Fatalf("expected value %v, got %v", testCase.expected, value)
			}
		})
	}
}

func TestSplitDryRunArgument(t *testing.T) {
	testCases := []struct {
		name       string
		args       []string
		dryRunFlag bool
		expected   []string
		override   *bool
	}{
		{name: "NoChange", args: []string{"sort"}, dryRunFlag: true, expected: []string{"sort"}},
		{name: "NoFlag", args: []string{"sort"}, dryRunFlag: false, expected: []string{"sort"}},
		{name: "BoolOverride", args: []string{"sort", "no"}, dryRunFlag: true, expected: []string{"sort"}, override: boolPointer(false)},
		{name: "DefaultRecipeOverride", args: []string{"no"}, dryRunFlag: true, expected: []string{}, override: boolPointer(false)},
		{name: "BoolNotRecognized", args: []string{"sort", "maybe"}, dryRunFlag: true, expected: []string{"sort", "maybe"}},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(testingT *testing.T) {
			trimmed, override := splitDryRunArgument(testCase.args, testCase.dryRunFlag)
			if override == nil && testCase.override != nil {
				testingT.Fatalf("expected override but got nil")
			}
			if override != nil && testCase.override == nil {
				testingT.Fatalf("expected no override but got one")
			}
			if override != nil && testCase.override != nil && *override != *testCase.override {
				testingT.Fatalf("expected override %v, got %v", *testCase.override, *override)
			}
			if len(trimmed) != len(testCase.expected) {
				testingT.Fatalf("expected args %v, got %v", testCase.expected, trimmed)
			}
			for index := range trimmed {
				if trimmed[index] != testCase.expected[index] {
					testingT.Fatalf("expected args %v, got %v", testCase.expected, trimmed)
				}
			}
		})
	}
}

func boolPointer(value bool) *bool {
	result := value
	return &result
}
