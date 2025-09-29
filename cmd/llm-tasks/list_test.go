package llmtasks

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

const sampleConfig = `
common:
  api:
    endpoint: https://api.openai.com/v1
    api_key_env: OPENAI_API_KEY
  defaults:
    attempts: 1
    timeout_seconds: 1

models:
  - name: gpt-5-mini
    provider: openai
    model_id: gpt-5-mini
    default: true
    supports_temperature: false
    default_temperature: 1
    max_completion_tokens: 1500

recipes:
  - name: changelog
    enabled: true
    model: gpt-5-mini
    type: task/changelog
    inputs: { }
    recipe: { }
    apply: { }
  - name: sort
    enabled: false
    model: gpt-5-mini
    type: task/sort
    grant: { }
    projects: [ ]
    thresholds: { }
`

func writeTempConfig(testingT *testing.T) string {
	testingT.Helper()
	temporaryDirectory := testingT.TempDir()
	configPath := filepath.Join(temporaryDirectory, "config.yaml")
	if err := os.WriteFile(configPath, []byte(sampleConfig), 0o644); err != nil {
		testingT.Fatalf("write config: %v", err)
	}
	return configPath
}

func TestRootListCommand(t *testing.T) {
	configPath := writeTempConfig(t)
	testCases := []struct {
		name                string
		arguments           []string
		expectedSubstrings  []string
		forbiddenSubstrings []string
	}{
		{
			name:                "DefaultFiltersDisabled",
			arguments:           []string{"list", "--config", configPath},
			expectedSubstrings:  []string{"changelog"},
			forbiddenSubstrings: []string{"sort"},
		},
		{
			name:                "AllShowsDisabled",
			arguments:           []string{"list", "--config", configPath, "--all"},
			expectedSubstrings:  []string{"changelog", "sort"},
			forbiddenSubstrings: []string{},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			command := NewRootCommand()
			var buffer bytes.Buffer
			command.SetOut(&buffer)
			command.SetErr(&buffer)
			command.SetArgs(testCase.arguments)

			if err := command.Execute(); err != nil {
				t.Fatalf("execute list command: %v\nstdout:\n%s", err, buffer.String())
			}

			outputBytes := buffer.Bytes()
			for _, substring := range testCase.expectedSubstrings {
				if !bytes.Contains(outputBytes, []byte(substring)) {
					t.Fatalf("expected substring %q in output:\n%s", substring, buffer.String())
				}
			}
			for _, substring := range testCase.forbiddenSubstrings {
				if bytes.Contains(outputBytes, []byte(substring)) {
					t.Fatalf("did not expect substring %q in output:\n%s", substring, buffer.String())
				}
			}
		})
	}
}
