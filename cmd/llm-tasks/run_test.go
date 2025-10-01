package llmtasks_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	llmtasks "github.com/temirov/llm-tasks/cmd/llm-tasks"
)

const (
	changelogVersionFlagIdentifier  = "version"
	changelogDateFlagIdentifier     = "date"
	openAIAPIKeyEnvName             = "OPENAI_API_KEY"
	changelogVersionEnvName         = "CHANGELOG_VERSION"
	changelogDateEnvName            = "CHANGELOG_DATE"
	changelogApplySummaryPrefix     = "prepended changelog to"
	changelogVersionHelpBaseline    = "Changelog version metadata (exported to CHANGELOG_VERSION)"
	changelogVersionRequiredSuffix  = "(mutually exclusive with --date)"
	changelogFallbackVersion        = "Unreleased"
	changelogTemplateDefaultVersion = ""
	changelogTemplateDefaultDate    = ""
	changelogConfigTemplate         = `common:
  api:
    endpoint: %s
    api_key_env: OPENAI_API_KEY
  defaults:
    attempts: 1
    timeout_seconds: 1

models:
  - name: stub
    provider: openai
    model_id: stub-model
    default: true
    supports_temperature: false
    default_temperature: 0.1
    max_completion_tokens: 1200

recipes:
  - name: changelog
    enabled: true
    model: stub
    type: task/changelog
    inputs:
      version:
        required: true
        env: CHANGELOG_VERSION
        default: "` + changelogTemplateDefaultVersion + `"
      date:
        required: true
        env: CHANGELOG_DATE
        default: "` + changelogTemplateDefaultDate + `"
      git_log:
        required: true
        source: stdin
    recipe:
      system: "System prompt"
      format:
        heading: "## [${version}] - ${date}"
        sections:
          - title: "Highlights"
            min: 1
          - title: "Features ✨"
          - title: "Improvements ⚙️"
        footer: ""
      rules: [ ]
    apply:
      output_path: %s
      mode: prepend
      ensure_blank_line: false
`
	openAIAPIKeyValue = "test-key"
)

type chatCompletionRequestPayload struct {
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

type chatCompletionResponsePayload struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

type repositorySetup struct {
	CommitToken string
	DateFlag    string
}

func TestRunCommandHelpAnnotatesChangelogRequirement(testingT *testing.T) {
	testCases := []struct {
		name           string
		arguments      []string
		expectRequired bool
	}{
		{
			name:           "PositionalArgument",
			arguments:      []string{"run", "changelog", "--help"},
			expectRequired: true,
		},
		{
			name:           "NameFlagArgument",
			arguments:      []string{"run", "--name", "changelog", "--help"},
			expectRequired: true,
		},
		{
			name:           "OtherRecipe",
			arguments:      []string{"run", "--help"},
			expectRequired: false,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		testingT.Run(testCase.name, func(subTestT *testing.T) {
			rootCommand := llmtasks.NewRootCommand()
			outputBuffer := bytes.Buffer{}
			rootCommand.SetOut(&outputBuffer)
			rootCommand.SetErr(&outputBuffer)
			rootCommand.SetArgs(testCase.arguments)

			executionErr := rootCommand.Execute()
			if executionErr != nil {
				subTestT.Fatalf("execute command: %v", executionErr)
			}

			helpOutput := outputBuffer.String()
			hasBaseline := strings.Contains(helpOutput, changelogVersionHelpBaseline)
			hasSuffix := strings.Contains(helpOutput, changelogVersionRequiredSuffix)
			containsRequired := hasBaseline && hasSuffix
			if containsRequired != testCase.expectRequired {
				if testCase.expectRequired {
					subTestT.Fatalf("expected changelog help output to annotate version flag, got: %s", helpOutput)
				}
				subTestT.Fatalf("expected non-changelog help output to omit annotation, got: %s", helpOutput)
			}
		})
	}
}

func TestRunCommandChangelogMetadataInjection(testingT *testing.T) {
	testCases := []struct {
		name              string
		versionFlag       string
		dateFlag          string
		prepareRepository func(t *testing.T, repositoryDir string) repositorySetup
		expectedVersion   string
		expectTodayDate   bool
		expectedDate      string
	}{
		{
			name: "AutoFromLatestTag",
			prepareRepository: func(t *testing.T, repositoryDir string) repositorySetup {
				createFile(t, repositoryDir, "base.txt", "base")
				runGitCommand(t, repositoryDir, "add", "base.txt")
				runGitCommand(t, repositoryDir, "commit", "-m", "initial release")
				runGitCommand(t, repositoryDir, "tag", "v1.0.0")
				createFile(t, repositoryDir, "feature.txt", "feature")
				runGitCommand(t, repositoryDir, "add", "feature.txt")
				runGitCommand(t, repositoryDir, "commit", "-m", "feat: add new feature")
				return repositorySetup{CommitToken: "feat: add new feature"}
			},
			expectedVersion: "v1.0.1",
			expectTodayDate: true,
		},
		{
			name:        "ExplicitVersionOverridesTag",
			versionFlag: "v0.9.0",
			prepareRepository: func(t *testing.T, repositoryDir string) repositorySetup {
				createFile(t, repositoryDir, "base.txt", "base")
				runGitCommand(t, repositoryDir, "add", "base.txt")
				runGitCommand(t, repositoryDir, "commit", "-m", "initial base")
				runGitCommand(t, repositoryDir, "tag", "v0.9.0")
				createFile(t, repositoryDir, "mid.txt", "mid")
				runGitCommand(t, repositoryDir, "add", "mid.txt")
				runGitCommand(t, repositoryDir, "commit", "-m", "feat: mid release")
				runGitCommand(t, repositoryDir, "tag", "v1.0.0")
				createFile(t, repositoryDir, "latest.txt", "latest")
				runGitCommand(t, repositoryDir, "add", "latest.txt")
				runGitCommand(t, repositoryDir, "commit", "-m", "fix: latest patch")
				return repositorySetup{CommitToken: "fix: latest patch"}
			},
			expectedVersion: "v0.9.0",
			expectTodayDate: true,
		},
		{
			name: "ExplicitDateIgnoresTags",
			prepareRepository: func(t *testing.T, repositoryDir string) repositorySetup {
				createFile(t, repositoryDir, "base.txt", "base")
				runGitCommand(t, repositoryDir, "add", "base.txt")
				runGitCommand(t, repositoryDir, "commit", "-m", "initial base")
				runGitCommand(t, repositoryDir, "tag", "v0.1.0")
				time.Sleep(2 * time.Second)
				since := time.Now().UTC().Format(time.RFC3339)
				time.Sleep(2 * time.Second)
				createFile(t, repositoryDir, "change.txt", "change")
				runGitCommand(t, repositoryDir, "add", "change.txt")
				runGitCommand(t, repositoryDir, "commit", "-m", "feat: change after date")
				return repositorySetup{CommitToken: "feat: change after date", DateFlag: since}
			},
			expectedVersion: changelogFallbackVersion,
			expectedDate:    "",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		testingT.Run(testCase.name, func(subTestT *testing.T) {
			repositoryDir := subTestT.TempDir()
			initializeGitRepository(subTestT, repositoryDir)

			repoSetup := testCase.prepareRepository(subTestT, repositoryDir)
			commitToken := repoSetup.CommitToken
			dateFlag := testCase.dateFlag
			if repoSetup.DateFlag != "" {
				dateFlag = repoSetup.DateFlag
			}

			subTestT.Setenv(openAIAPIKeyEnvName, openAIAPIKeyValue)
			subTestT.Setenv(changelogVersionEnvName, "")
			subTestT.Setenv(changelogDateEnvName, "")

			var serverInteractions struct {
				prompt string
			}

			mockServer := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
				var payload chatCompletionRequestPayload
				if decodeErr := json.NewDecoder(request.Body).Decode(&payload); decodeErr != nil {
					subTestT.Fatalf("decode chat request: %v", decodeErr)
				}
				if len(payload.Messages) < 2 {
					subTestT.Fatalf("expected at least two messages, got %d", len(payload.Messages))
				}
				serverInteractions.prompt = payload.Messages[1].Content
				responseHeading := fmt.Sprintf("## [%s] - %s", os.Getenv(changelogVersionEnvName), os.Getenv(changelogDateEnvName))
				if responseHeading == "## [] - " {
					subTestT.Fatalf("expected heading data to be set before LLM request")
				}
				draft := fmt.Sprintf(`%s

### Highlights

- Auto item

### Features ✨

- Auto feature

### Improvements ⚙️

- Auto improvement

### Docs 📚

- Auto docs

### CI & Maintenance

- Auto maintenance

**Upgrade notes:** No breaking changes.
`, responseHeading)
				responsePayload := chatCompletionResponsePayload{
					Choices: []struct {
						Message struct {
							Content string `json:"content"`
						} `json:"message"`
					}{
						{
							Message: struct {
								Content string `json:"content"`
							}{Content: draft},
						},
					},
				}
				responseWriter.Header().Set("Content-Type", "application/json")
				if encodeErr := json.NewEncoder(responseWriter).Encode(responsePayload); encodeErr != nil {
					subTestT.Fatalf("encode chat response: %v", encodeErr)
				}
			}))
			defer mockServer.Close()

			configPath := filepath.Join(repositoryDir, "config.yaml")
			outputPath := filepath.Join(repositoryDir, "CHANGELOG.md")
			configContent := fmt.Sprintf(changelogConfigTemplate, mockServer.URL, outputPath)
			if writeErr := os.WriteFile(configPath, []byte(configContent), 0o600); writeErr != nil {
				subTestT.Fatalf("write config: %v", writeErr)
			}

			originalDir, dirErr := os.Getwd()
			if dirErr != nil {
				subTestT.Fatalf("getwd: %v", dirErr)
			}
			if chdirErr := os.Chdir(repositoryDir); chdirErr != nil {
				subTestT.Fatalf("chdir: %v", chdirErr)
			}
			subTestT.Cleanup(func() { _ = os.Chdir(originalDir) })

			command := llmtasks.NewRootCommand()
			command.SetArgs(buildRunArguments(configPath, testCase.versionFlag, dateFlag))
			var outputBuffer bytes.Buffer
			command.SetOut(&outputBuffer)
			command.SetErr(&outputBuffer)

			executeErr := command.Execute()
			if executeErr != nil {
				subTestT.Fatalf("execute run command: %v\noutput:%s", executeErr, outputBuffer.String())
			}

			if !strings.Contains(outputBuffer.String(), changelogApplySummaryPrefix) {
				subTestT.Fatalf("expected apply summary in output, got %s", outputBuffer.String())
			}

			changelogData, readErr := os.ReadFile(outputPath)
			if readErr != nil {
				subTestT.Fatalf("read changelog: %v", readErr)
			}
			if !strings.Contains(string(changelogData), "### Highlights") {
				subTestT.Fatalf("expected changelog to contain Highlights heading")
			}

			expectedVersion := testCase.expectedVersion
			if expectedVersion == "" {
				expectedVersion = changelogFallbackVersion
			}
			if os.Getenv(changelogVersionEnvName) != expectedVersion {
				subTestT.Fatalf("expected CHANGELOG_VERSION=%s, got %s", expectedVersion, os.Getenv(changelogVersionEnvName))
			}

			var expectedDate string
			if testCase.expectTodayDate {
				expectedDate = time.Now().UTC().Format(time.DateOnly)
			} else if repoSetup.DateFlag != "" {
				expectedDate = repoSetup.DateFlag
			} else if testCase.expectedDate != "" {
				expectedDate = testCase.expectedDate
			} else {
				expectedDate = time.Now().UTC().Format(time.DateOnly)
			}
			if os.Getenv(changelogDateEnvName) != expectedDate {
				subTestT.Fatalf("expected CHANGELOG_DATE=%s, got %s", expectedDate, os.Getenv(changelogDateEnvName))
			}

			if !strings.Contains(serverInteractions.prompt, commitToken) {
				subTestT.Fatalf("expected LLM prompt to contain commit token %q", commitToken)
			}
		})
	}
}

func TestRunCommandChangelogFailsWithNoCommits(testingT *testing.T) {
	repositoryDir := testingT.TempDir()
	initializeGitRepository(testingT, repositoryDir)

	createFile(testingT, repositoryDir, "base.txt", "base")
	runGitCommand(testingT, repositoryDir, "add", "base.txt")
	runGitCommand(testingT, repositoryDir, "commit", "-m", "initial release")
	runGitCommand(testingT, repositoryDir, "tag", "v0.1.0")

	testingT.Setenv(openAIAPIKeyEnvName, openAIAPIKeyValue)

	configPath := filepath.Join(repositoryDir, "config.yaml")
	changelogPath := filepath.Join(repositoryDir, "CHANGELOG.md")
	configContent := fmt.Sprintf(changelogConfigTemplate, "http://127.0.0.1", changelogPath)
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		testingT.Fatalf("write config: %v", err)
	}

	originalDir, dirErr := os.Getwd()
	if dirErr != nil {
		testingT.Fatalf("getwd: %v", dirErr)
	}
	if chdirErr := os.Chdir(repositoryDir); chdirErr != nil {
		testingT.Fatalf("chdir: %v", chdirErr)
	}
	testingT.Cleanup(func() { _ = os.Chdir(originalDir) })

	command := llmtasks.NewRootCommand()
	command.SetArgs([]string{"run", "changelog", "--config", configPath})
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})

	executeErr := command.Execute()
	if executeErr == nil {
		testingT.Fatalf("expected command to fail due to empty range")
	}
	if !strings.Contains(executeErr.Error(), "no git changes detected") {
		testingT.Fatalf("expected no changes error, got: %v", executeErr)
	}
}

func buildRunArguments(configPath, versionFlag, dateFlag string) []string {
	arguments := []string{"run", "changelog", "--config", configPath}
	if strings.TrimSpace(versionFlag) != "" {
		arguments = append(arguments, "--"+changelogVersionFlagIdentifier, versionFlag)
	}
	if strings.TrimSpace(dateFlag) != "" {
		arguments = append(arguments, "--"+changelogDateFlagIdentifier, dateFlag)
	}
	return arguments
}

func initializeGitRepository(t *testing.T, dir string) {
	t.Helper()
	runGitCommand(t, dir, "init")
	runGitCommand(t, dir, "config", "user.email", "ci@example.com")
	runGitCommand(t, dir, "config", "user.name", "CI User")
}

func createFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func runGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	gitCmd := exec.Command("git", args...)
	gitCmd.Dir = dir
	if output, err := gitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}
