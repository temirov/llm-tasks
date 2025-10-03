package llmtasks_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
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
	openAIAPIKeyValue       = "test-key"
	sortedFilesResponseKey  = "sorted_files"
	cliOverrideProjectName  = "CLI_Override_Project"
	cliOverrideTargetSubdir = "Overrides/Check"
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
		name              string
		arguments         []string
		expectRequired    bool
		expectVersionFlag bool
	}{
		{
			name:              "PositionalArgument",
			arguments:         []string{"run", "changelog", "--help"},
			expectRequired:    true,
			expectVersionFlag: true,
		},
		{
			name:              "NameFlagArgument",
			arguments:         []string{"run", "--name", "changelog", "--help"},
			expectRequired:    true,
			expectVersionFlag: true,
		},
		{
			name:              "OtherRecipe",
			arguments:         []string{"run", "--help"},
			expectRequired:    false,
			expectVersionFlag: false,
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
			if hasBaseline != testCase.expectVersionFlag {
				if testCase.expectVersionFlag {
					subTestT.Fatalf("expected help output to include version flag, got: %s", helpOutput)
				}
				subTestT.Fatalf("expected help output to hide version flag, got: %s", helpOutput)
			}
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

func TestRunCommandSortHelpShowsInputs(testingT *testing.T) {
	rootCommand := llmtasks.NewRootCommand()
	var outputBuffer bytes.Buffer
	rootCommand.SetOut(&outputBuffer)
	rootCommand.SetErr(&outputBuffer)
	rootCommand.SetArgs([]string{"run", "sort", "--help"})

	executionErr := rootCommand.Execute()
	if executionErr != nil {
		testingT.Fatalf("execute command: %v", executionErr)
	}

	helpOutput := outputBuffer.String()
	if !strings.Contains(helpOutput, "--source") {
		testingT.Fatalf("expected help output to list --source, got: %s", helpOutput)
	}
	if !strings.Contains(helpOutput, "--destination") {
		testingT.Fatalf("expected help output to list --destination, got: %s", helpOutput)
	}
}

func TestRunCommandSortCliOverridesConfig(t *testing.T) {
	rootWorkingDirectory := t.TempDir()
	configDownloadsDirectory := filepath.Join(rootWorkingDirectory, "config-downloads")
	configStagingDirectory := filepath.Join(rootWorkingDirectory, "config-staging")
	cliDownloadsDirectory := filepath.Join(rootWorkingDirectory, "cli-downloads")
	cliStagingDirectory := filepath.Join(rootWorkingDirectory, "cli-staging")
	directoryList := []string{configDownloadsDirectory, configStagingDirectory, cliDownloadsDirectory, cliStagingDirectory}
	for _, directoryPath := range directoryList {
		if err := os.MkdirAll(directoryPath, 0o755); err != nil {
			t.Fatalf("create directory: %v", err)
		}
	}
	configSourceFileName := "config-source.txt"
	cliSourceFileName := "cli-source.txt"
	createFile(t, configDownloadsDirectory, configSourceFileName, "config")
	createFile(t, cliDownloadsDirectory, cliSourceFileName, "cli")

	var requestCounter int64
	stubServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		atomic.AddInt64(&requestCounter, 1)
		defer func() { _ = request.Body.Close() }()
		var requestPayload chatCompletionRequestPayload
		if err := json.NewDecoder(request.Body).Decode(&requestPayload); err != nil {
			t.Fatalf("decode request payload: %v", err)
		}
		if len(requestPayload.Messages) == 0 {
			t.Fatalf("expected chat completion request to include messages")
		}
		userMessage := requestPayload.Messages[len(requestPayload.Messages)-1].Content
		promptFiles := extractPromptFileMetadata(t, userMessage)
		if len(promptFiles) != 1 {
			t.Fatalf("expected exactly one prompt file, got %d", len(promptFiles))
		}
		promptFile := promptFiles[0]
		if promptFile.Name != cliSourceFileName {
			t.Fatalf("expected CLI file %s, got %s", cliSourceFileName, promptFile.Name)
		}
		classificationResults := []map[string]string{
			{
				"file_name":     promptFile.Name,
				"project_name":  cliOverrideProjectName,
				"target_subdir": cliOverrideTargetSubdir,
			},
		}
		classificationPayload, err := json.Marshal(map[string]any{sortedFilesResponseKey: classificationResults})
		if err != nil {
			t.Fatalf("marshal classification payload: %v", err)
		}
		responseBody, err := json.Marshal(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": string(classificationPayload),
						"role":    "assistant",
					},
					"finish_reason": "stop",
				},
			},
		})
		if err != nil {
			t.Fatalf("marshal response payload: %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		if _, err := writer.Write(responseBody); err != nil {
			t.Fatalf("write response body: %v", err)
		}
	}))
	defer stubServer.Close()

	configTemplate := fmt.Sprintf(`common:
  api:
    endpoint: %s
    api_key_env: %s
  defaults:
    attempts: 1
    timeout_seconds: 5

models:
  - name: stub
    provider: openai
    model_id: stub-model
    default: true
    supports_temperature: false
    default_temperature: 0.0
    max_completion_tokens: 128

recipes:
  - name: sort
    enabled: true
    model: stub
    grant:
      base_directories:
        downloads: %s
        staging: %s
      safety:
        dry_run: true
    projects:
      - name: "CLI Override"
        target: "CLI Override"
        keywords: ["txt"]
    thresholds:
      min_confidence: 0.1
`, strconv.Quote(stubServer.URL), strconv.Quote(openAIAPIKeyEnvName), strconv.Quote(configDownloadsDirectory), strconv.Quote(configStagingDirectory))
	configPath := filepath.Join(rootWorkingDirectory, "root-config.yaml")
	if err := os.WriteFile(configPath, []byte(configTemplate), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	previousAPIKey := os.Getenv(openAIAPIKeyEnvName)
	if err := os.Setenv(openAIAPIKeyEnvName, openAIAPIKeyValue); err != nil {
		t.Fatalf("set API key env: %v", err)
	}
	t.Cleanup(func() {
		if strings.TrimSpace(previousAPIKey) == "" {
			_ = os.Unsetenv(openAIAPIKeyEnvName)
			return
		}
		_ = os.Setenv(openAIAPIKeyEnvName, previousAPIKey)
	})

	rootCommand := llmtasks.NewRootCommand()
	var commandOutput bytes.Buffer
	var commandErrors bytes.Buffer
	rootCommand.SetOut(&commandOutput)
	rootCommand.SetErr(&commandErrors)
	rootCommand.SetArgs([]string{
		"run",
		"sort",
		"--config", configPath,
		"--source", cliDownloadsDirectory,
		"--destination", cliStagingDirectory,
		"--dry-run", "yes",
	})

	capturedStdout, executeErr := captureStdout(t, func() error {
		return rootCommand.Execute()
	})
	if executeErr != nil {
		t.Fatalf("execute command: %v (stdout=%s stderr=%s)", executeErr, capturedStdout, commandErrors.String())
	}
	if commandErrors.Len() > 0 {
		t.Fatalf("unexpected stderr output: %s", commandErrors.String())
	}

	if atomic.LoadInt64(&requestCounter) == 0 {
		t.Fatalf("expected LLM stub to receive at least one request")
	}

	if !strings.Contains(commandOutput.String(), "actions=1") {
		t.Fatalf("expected command output to report one action, got: %s", commandOutput.String())
	}
	if !strings.Contains(commandOutput.String(), "dry=true") {
		t.Fatalf("expected command output to report dry run, got: %s", commandOutput.String())
	}

	expectedSourcePath := filepath.Join(cliDownloadsDirectory, cliSourceFileName)
	overrideSubdirComponents := strings.Split(cliOverrideTargetSubdir, "/")
	destinationComponents := append([]string{cliStagingDirectory}, overrideSubdirComponents...)
	destinationComponents = append(destinationComponents, cliSourceFileName)
	expectedDestinationPath := filepath.Join(destinationComponents...)
	if !strings.Contains(capturedStdout, expectedSourcePath) {
		t.Fatalf("expected stdout to include source path %s, got: %s", expectedSourcePath, capturedStdout)
	}
	if !strings.Contains(capturedStdout, expectedDestinationPath) {
		t.Fatalf("expected stdout to include destination path %s, got: %s", expectedDestinationPath, capturedStdout)
	}
	if !strings.Contains(capturedStdout, cliOverrideProjectName) {
		t.Fatalf("expected stdout to include project name %s, got: %s", cliOverrideProjectName, capturedStdout)
	}
	if strings.Contains(capturedStdout, configDownloadsDirectory) {
		t.Fatalf("expected stdout to avoid config downloads path, got: %s", capturedStdout)
	}
	if strings.Contains(capturedStdout, configStagingDirectory) {
		t.Fatalf("expected stdout to avoid config staging path, got: %s", capturedStdout)
	}
	if strings.Contains(capturedStdout, configSourceFileName) {
		t.Fatalf("expected stdout to avoid config file name, got: %s", capturedStdout)
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

type promptFileMetadata struct {
	Name string `json:"name"`
}

func captureStdout(t *testing.T, execute func() error) (string, error) {
	t.Helper()
	reader, writer, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("create stdout pipe: %v", pipeErr)
	}
	originalStdout := os.Stdout
	os.Stdout = writer
	executeErr := execute()
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatalf("close stdout writer: %v", closeErr)
	}
	os.Stdout = originalStdout
	capturedBytes, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatalf("read captured stdout: %v", readErr)
	}
	if closeErr := reader.Close(); closeErr != nil {
		t.Fatalf("close stdout reader: %v", closeErr)
	}
	return string(capturedBytes), executeErr
}

func extractPromptFileMetadata(t *testing.T, userContent string) []promptFileMetadata {
	t.Helper()
	metadataMarker := "File metadata (array):"
	metadataMarkerIndex := strings.Index(userContent, metadataMarker)
	if metadataMarkerIndex < 0 {
		t.Fatalf("user prompt missing metadata marker: %s", userContent)
	}
	metadataSection := userContent[metadataMarkerIndex+len(metadataMarker):]
	metadataSection = strings.TrimSpace(metadataSection)
	sectionParts := strings.SplitN(metadataSection, "\n\nRules:", 2)
	if len(sectionParts) < 2 {
		t.Fatalf("user prompt missing rules separator: %s", userContent)
	}
	metadataJSON := strings.TrimSpace(sectionParts[0])
	if metadataJSON == "" {
		t.Fatalf("user prompt metadata section empty: %s", userContent)
	}
	var promptFiles []promptFileMetadata
	if err := json.Unmarshal([]byte(metadataJSON), &promptFiles); err != nil {
		t.Fatalf("parse prompt metadata: %v", err)
	}
	return promptFiles
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
