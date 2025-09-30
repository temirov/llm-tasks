package llmtasks_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"gopkg.in/yaml.v3"

	llmtasks "github.com/temirov/llm-tasks/cmd/llm-tasks"
	"github.com/temirov/llm-tasks/internal/config"
)

const (
	openAIAPIKeyEnvironmentVariable     = "OPENAI_API_KEY"
	changelogVersionEnvironmentVariable = "CHANGELOG_VERSION"
	changelogDateEnvironmentVariable    = "CHANGELOG_DATE"
	changelogRecipeName                 = "changelog"
	changelogRecipeTypeValue            = "task/changelog"
	highlightSectionTitle               = "Highlights"
	detailsSectionTitle                 = "Details"
	chatCompletionPath                  = "/chat/completions"
	responseContentTypeJSON             = "application/json"
	testModelName                       = "test-model"
	testModelIdentifier                 = "gpt-test"
	loggingLevelInfo                    = "info"
	loggingFormatText                   = "text"
	systemPromptValue                   = "System instructions for changelog generation."
	headingTemplate                     = "## Release ${version} (${date})"
	ruleInstruction                     = "Summarize git log into Markdown."
	applyModePrint                      = "print"
)

type testChangelogConfigBuilder struct {
	endpoint string
}

func newTestChangelogConfigBuilder(endpoint string) testChangelogConfigBuilder {
	return testChangelogConfigBuilder{endpoint: endpoint}
}

func (builder testChangelogConfigBuilder) writeConfig(t *testing.T, directory string) string {
	t.Helper()

	rootConfiguration := config.Root{}
	rootConfiguration.Common.API.Endpoint = builder.endpoint
	rootConfiguration.Common.API.APIKeyEnv = openAIAPIKeyEnvironmentVariable
	rootConfiguration.Common.Logging.Level = loggingLevelInfo
	rootConfiguration.Common.Logging.Format = loggingFormatText
	rootConfiguration.Common.Defaults.Attempts = 1
	rootConfiguration.Common.Defaults.TimeoutSeconds = 1

	rootConfiguration.Models = []config.Model{
		{
			Name:                testModelName,
			Provider:            "openai",
			ModelID:             testModelIdentifier,
			Default:             true,
			SupportsTemperature: false,
			DefaultTemperature:  0,
			MaxCompletionTokens: 1024,
		},
	}

	recipeBody := builder.createRecipeBody()
	rootConfiguration.Recipes = []config.Recipe{
		{
			Name:    changelogRecipeName,
			Enabled: true,
			Model:   testModelName,
			Type:    changelogRecipeTypeValue,
			Body:    marshalChangelogRecipeBody(t, recipeBody),
		},
	}

	configData, marshalErr := yaml.Marshal(rootConfiguration)
	if marshalErr != nil {
		t.Fatalf("marshal config: %v", marshalErr)
	}

	configPath := filepath.Join(directory, "config.yaml")
	if writeErr := os.WriteFile(configPath, configData, 0o600); writeErr != nil {
		t.Fatalf("write config: %v", writeErr)
	}

	return configPath
}

func (builder testChangelogConfigBuilder) createRecipeBody() changelogRecipeBody {
	recipeBody := changelogRecipeBody{}
	recipeBody.Inputs.Version.Required = true
	recipeBody.Inputs.Version.Env = changelogVersionEnvironmentVariable
	recipeBody.Inputs.Version.Default = ""
	recipeBody.Inputs.Date.Required = true
	recipeBody.Inputs.Date.Env = changelogDateEnvironmentVariable
	recipeBody.Inputs.Date.Default = ""
	recipeBody.Inputs.GitLog.Required = false
	recipeBody.Inputs.GitLog.Source = ""

	recipeBody.Recipe.System = systemPromptValue
	recipeBody.Recipe.Format.Heading = headingTemplate
	recipeBody.Recipe.Format.Sections = []changelogFormatSectionDefinition{
		{
			Title: highlightSectionTitle,
			Min:   1,
			Max:   3,
		},
		{
			Title: detailsSectionTitle,
		},
	}
	recipeBody.Recipe.Format.Footer = ""
	recipeBody.Recipe.Rules = []string{ruleInstruction}

	recipeBody.Apply.OutputPath = ""
	recipeBody.Apply.Mode = applyModePrint
	recipeBody.Apply.EnsureBlankLine = false

	return recipeBody
}

func marshalChangelogRecipeBody(t *testing.T, body changelogRecipeBody) map[string]any {
	t.Helper()

	bodyData, marshalErr := yaml.Marshal(body)
	if marshalErr != nil {
		t.Fatalf("marshal recipe body: %v", marshalErr)
	}

	var bodyMap map[string]any
	if unmarshalErr := yaml.Unmarshal(bodyData, &bodyMap); unmarshalErr != nil {
		t.Fatalf("unmarshal recipe body: %v", unmarshalErr)
	}

	return bodyMap
}

type changelogRecipeBody struct {
	Inputs changelogInputs           `yaml:"inputs"`
	Recipe changelogRecipeDefinition `yaml:"recipe"`
	Apply  changelogApplyDefinition  `yaml:"apply"`
}

type changelogInputs struct {
	Version changelogInputDefinition `yaml:"version"`
	Date    changelogInputDefinition `yaml:"date"`
	GitLog  struct {
		Required bool   `yaml:"required"`
		Source   string `yaml:"source"`
	} `yaml:"git_log"`
}

type changelogInputDefinition struct {
	Required bool   `yaml:"required"`
	Env      string `yaml:"env"`
	Default  string `yaml:"default"`
}

type changelogRecipeDefinition struct {
	System string                    `yaml:"system"`
	Format changelogFormatDefinition `yaml:"format"`
	Rules  []string                  `yaml:"rules"`
}

type changelogFormatDefinition struct {
	Heading  string                             `yaml:"heading"`
	Sections []changelogFormatSectionDefinition `yaml:"sections"`
	Footer   string                             `yaml:"footer"`
}

type changelogFormatSectionDefinition struct {
	Title string `yaml:"title"`
	Min   int    `yaml:"min,omitempty"`
	Max   int    `yaml:"max,omitempty"`
}

type changelogApplyDefinition struct {
	OutputPath      string `yaml:"output_path"`
	Mode            string `yaml:"mode"`
	EnsureBlankLine bool   `yaml:"ensure_blank_line"`
}

func buildChangelogResponse(version string, date string) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("## Release %s (%s)\n\n", version, date))
	builder.WriteString(fmt.Sprintf("### %s\n\n", highlightSectionTitle))
	builder.WriteString("- Added new feature.\n\n")
	builder.WriteString(fmt.Sprintf("### %s\n\n", detailsSectionTitle))
	builder.WriteString("- Additional implementation detail.\n")
	return builder.String()
}

func TestRunCommandChangelogFlags(t *testing.T) {
	scenarios := []struct {
		name            string
		versionFlag     string
		dateFlag        string
		expectedVersion string
		expectedDate    string
	}{
		{
			name:            "uses provided version and date flags",
			versionFlag:     "  v1.2.3  ",
			dateFlag:        " 2024-07-01 ",
			expectedVersion: "v1.2.3",
			expectedDate:    "2024-07-01",
		},
	}

	for _, scenario := range scenarios {
		scenario := scenario
		t.Run(scenario.name, func(t *testing.T) {

			var requestCount int32
			mockServer := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, httpRequest *http.Request) {
				if httpRequest.URL.Path != chatCompletionPath {
					t.Fatalf("unexpected request path: %s", httpRequest.URL.Path)
				}

				atomic.AddInt32(&requestCount, 1)

				requestBody, readErr := io.ReadAll(httpRequest.Body)
				if readErr != nil {
					t.Fatalf("read request body: %v", readErr)
				}
				if closeErr := httpRequest.Body.Close(); closeErr != nil {
					t.Fatalf("close request body: %v", closeErr)
				}

				var payload struct {
					Messages []struct {
						Role    string `json:"role"`
						Content string `json:"content"`
					} `json:"messages"`
				}
				if decodeErr := json.Unmarshal(requestBody, &payload); decodeErr != nil {
					t.Fatalf("decode request: %v", decodeErr)
				}
				if len(payload.Messages) == 0 {
					t.Fatalf("no messages in request payload")
				}
				userMessage := payload.Messages[len(payload.Messages)-1].Content
				if !strings.Contains(userMessage, scenario.expectedVersion) {
					t.Fatalf("user prompt missing version: %s", userMessage)
				}
				if !strings.Contains(userMessage, scenario.expectedDate) {
					t.Fatalf("user prompt missing date: %s", userMessage)
				}

				responsePayload := map[string]any{
					"choices": []map[string]any{
						{
							"message": map[string]any{
								"role":    "assistant",
								"content": buildChangelogResponse(scenario.expectedVersion, scenario.expectedDate),
							},
						},
					},
				}
				responseBytes, marshalErr := json.Marshal(responsePayload)
				if marshalErr != nil {
					t.Fatalf("marshal response: %v", marshalErr)
				}

				responseWriter.Header().Set("Content-Type", responseContentTypeJSON)
				responseWriter.WriteHeader(http.StatusOK)
				if _, writeErr := responseWriter.Write(responseBytes); writeErr != nil {
					t.Fatalf("write response: %v", writeErr)
				}
			}))
			t.Cleanup(mockServer.Close)

			t.Setenv(openAIAPIKeyEnvironmentVariable, "test-key")
			t.Setenv(changelogVersionEnvironmentVariable, "")
			t.Setenv(changelogDateEnvironmentVariable, "")

			tempDir := t.TempDir()
			configBuilder := newTestChangelogConfigBuilder(mockServer.URL)
			configPath := configBuilder.writeConfig(t, tempDir)

			rootCommand := llmtasks.NewRootCommand()
			rootCommand.SetArgs([]string{
				"run",
				changelogRecipeName,
				"--config", configPath,
				"--version", scenario.versionFlag,
				"--date", scenario.dateFlag,
			})
			rootCommand.SetIn(strings.NewReader("feat: sample change\n"))
			var commandOutput bytes.Buffer
			rootCommand.SetOut(&commandOutput)
			rootCommand.SetErr(&commandOutput)

			executionErr := rootCommand.Execute()
			if executionErr != nil {
				t.Fatalf("execute command: %v\nOutput: %s", executionErr, commandOutput.String())
			}

			if atomic.LoadInt32(&requestCount) == 0 {
				t.Fatalf("expected at least one LLM request")
			}
		})
	}
}
