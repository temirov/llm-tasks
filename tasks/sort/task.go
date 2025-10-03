package sort

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/temirov/llm-tasks/internal/config"
	"github.com/temirov/llm-tasks/internal/fsops"
	"github.com/temirov/llm-tasks/internal/pipeline"
)

const sortCompletionMaxTokens = 768

const sortedFilesKey = pipeline.SortedFilesSchemaName

const sortJSONSchema = "{\n" +
	"  \"type\": \"object\",\n" +
	"  \"properties\": {\n" +
	"    \"" + sortedFilesKey + "\": {\n" +
	"      \"type\": \"array\",\n" +
	"      \"items\": {\n" +
	"        \"type\": \"object\",\n" +
	"        \"required\": [\"file_name\", \"project_name\", \"target_subdir\"],\n" +
	"        \"properties\": {\n" +
	"          \"file_name\": {\"type\": \"string\", \"minLength\": 1, \"maxLength\": 160},\n" +
	"          \"project_name\": {\"type\": \"string\", \"minLength\": 2, \"maxLength\": 64},\n" +
	"          \"target_subdir\": {\"type\": \"string\", \"minLength\": 1, \"maxLength\": 200}\n" +
	"        },\n" +
	"        \"additionalProperties\": false\n" +
	"      }\n" +
	"    }\n" +
	"  },\n" +
	"  \"required\": [\"" + sortedFilesKey + "\"],\n" +
	"  \"additionalProperties\": false\n" +
	"}"

type Task struct {
	fs      fsops.Ops
	cfgProv SortConfigProvider

	Inventory          []FileMeta
	Plan               MovePlan
	downloadsRoot      string
	stagingRoot        string
	preloadedInventory []FileMeta
	completionTokens   int
	lastRequest        pipeline.LLMRequest
	lastResponse       pipeline.LLMResponse
	overrideDownloads  string
	overrideStaging    string
	dryRunOverride     *bool
	currentDryRun      bool
}

func New() pipeline.Pipeline {
	return NewWithDeps(
		fsops.NewOps(fsops.NewOS()),
		FileSortConfigProvider{Path: "configs/task.sort.yaml"},
	)
}

func NewWithDeps(fs fsops.Ops, cfg SortConfigProvider) pipeline.Pipeline {
	return &Task{fs: fs, cfgProv: cfg, completionTokens: sortCompletionMaxTokens}
}

// DefaultFS exported for wiring from the runner
func DefaultFS() fsops.Ops { return fsops.NewOps(fsops.NewOS()) }

type FileMeta struct {
	AbsolutePath   string            `json:"absolute_path"`
	RelativePath   string            `json:"path"`
	BaseName       string            `json:"base_name"`
	Extension      string            `json:"extension"`
	MIMEType       string            `json:"mime"`
	SizeBytes      int64             `json:"size_bytes"`
	ArchiveEntries []ArchiveEntry    `json:"archive_entries,omitempty"`
	ImageMetadata  map[string]string `json:"image_metadata,omitempty"`
}

type ArchiveEntry struct {
	Path      string `json:"path"`
	MIMEType  string `json:"mime"`
	SizeBytes int64  `json:"size_bytes"`
}

type LLMResult struct {
	FileName     string `json:"file_name"`
	ProjectName  string `json:"project_name"`
	TargetSubdir string `json:"target_subdir"`
}

type MoveAction struct {
	FromPath    string `json:"from"`
	ToPath      string `json:"to"`
	FileName    string `json:"file_name"`
	ProjectName string `json:"project_name"`
}

type MovePlan struct {
	Actions []MoveAction `json:"actions"`
	DryRun  bool         `json:"dry_run"`
}

var errMissingSortedFilesKey = errors.New("missing sorted files key")

func (t *Task) Name() string { return "sort" }

type promptFile struct {
	Name       string            `json:"name"`
	Folder     string            `json:"folder,omitempty"`
	Extension  string            `json:"extension"`
	MIMEType   string            `json:"mime,omitempty"`
	SizeKB     int64             `json:"size_kb"`
	Archive    []string          `json:"archive,omitempty"`
	ArchiveToo bool              `json:"archive_truncated,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

func (t *Task) Clone() *Task {
	clone := &Task{
		fs:               t.fs,
		cfgProv:          t.cfgProv,
		completionTokens: t.completionTokens,
	}
	if len(t.preloadedInventory) > 0 {
		clone.preloadedInventory = append([]FileMeta(nil), t.preloadedInventory...)
	}
	return clone
}

func (t *Task) Preload(files []FileMeta) {
	t.preloadedInventory = append([]FileMeta(nil), files...)
}

func (t *Task) SetCompletionTokens(tokens int) {
	if tokens > 0 {
		t.completionTokens = tokens
	}
}

func (t *Task) SetBaseDirectories(downloads, staging string) error {
	if trimmed := strings.TrimSpace(downloads); trimmed != "" {
		normalized, err := normalizePath(trimmed)
		if err != nil {
			return fmt.Errorf("normalize source directory: %w", err)
		}
		t.overrideDownloads = normalized
	}
	if trimmed := strings.TrimSpace(staging); trimmed != "" {
		normalized, err := normalizePath(trimmed)
		if err != nil {
			return fmt.Errorf("normalize destination directory: %w", err)
		}
		t.overrideStaging = normalized
	}
	return nil
}

func (t *Task) SetDryRunOverride(dry bool) {
	value := dry
	t.dryRunOverride = &value
}

// 1) Gather
func (t *Task) Gather(ctx context.Context) (pipeline.GatherOutput, error) {
	cfg, err := t.cfgProv.Load()
	if err != nil {
		return nil, err
	}
	if t.overrideDownloads != "" {
		cfg.Grant.BaseDirectories.Downloads = t.overrideDownloads
	}
	if t.overrideStaging != "" {
		cfg.Grant.BaseDirectories.Staging = t.overrideStaging
	}
	if t.dryRunOverride != nil {
		cfg.Grant.Safety.DryRun = *t.dryRunOverride
	}

	t.downloadsRoot = strings.TrimSpace(cfg.Grant.BaseDirectories.Downloads)
	t.stagingRoot = strings.TrimSpace(cfg.Grant.BaseDirectories.Staging)
	t.currentDryRun = cfg.Grant.Safety.DryRun
	if err := validateBaseDirectories(t.downloadsRoot, t.stagingRoot); err != nil {
		return nil, err
	}
	if len(t.preloadedInventory) > 0 {
		copyOf := append([]FileMeta(nil), t.preloadedInventory...)
		t.preloadedInventory = nil
		t.Inventory = copyOf
		return copyOf, nil
	}
	infos, err := t.fs.Inventory(t.downloadsRoot)
	if err != nil {
		return nil, err
	}
	result := make([]FileMeta, 0, len(infos))
	for _, info := range infos {
		relativePath := displayRelativePath(cfg.Grant.BaseDirectories.Downloads, info.AbsolutePath)
		entries, inspectErr := collectArchiveEntries(t.fs.FS, info)
		if inspectErr != nil {
			return nil, fmt.Errorf("inspect archive %s: %w", info.AbsolutePath, inspectErr)
		}
		imageMetadata := collectImageMetadata(info)
		result = append(result, FileMeta{
			AbsolutePath:   info.AbsolutePath,
			RelativePath:   relativePath,
			BaseName:       info.BaseName,
			Extension:      info.Extension,
			MIMEType:       info.MIMEType,
			SizeBytes:      info.SizeBytes,
			ArchiveEntries: entries,
			ImageMetadata:  imageMetadata,
		})
	}
	t.Inventory = result
	return result, nil
}

// 2) Prompt
func (t *Task) Prompt(ctx context.Context, gathered pipeline.GatherOutput) (pipeline.LLMRequest, error) {
	files := gathered.([]FileMeta)
	promptPayload := buildPromptFiles(files)
	filesJSON, _ := json.Marshal(promptPayload)

	system := strings.TrimSpace(fmt.Sprintf(`
You classify files into project folders using only the provided metadata.
Return JSON object with key %q containing one array entry per file in the same order.
Each array entry must contain exactly: file_name, project_name, target_subdir.
No commentary, no code fences, no extra keys.
`, sortedFilesKey))

	user := fmt.Sprintf(`Downloads root: %s
Staging root: %s

Existing projects:
%s

File metadata (array):
%s

Rules:
- Respond with JSON object containing key %s only.
- %s must be an array with one object per file in the same order.
- file_name must copy the original file name (with extension).
- project_name must stay under 60 characters and use letters, numbers, spaces, dashes, or underscores.
- target_subdir is the relative folder path under the staging root using forward slashes only.
- Do not introduce extra keys or commentary.
`, t.currentDownloadsRoot(), t.currentStagingRoot(), t.loadProjectListJSON(), sortedFilesKey, sortedFilesKey, string(filesJSON))

	req := pipeline.LLMRequest{
		SystemPrompt: system,
		UserPrompt:   user,
		MaxTokens:    t.completionTokens,
		Temperature:  0.1,
		JSONSchema:   []byte(sortJSONSchema),
	}
	t.lastRequest = req
	return req, nil
}

func buildPromptFiles(files []FileMeta) []promptFile {
	result := make([]promptFile, 0, len(files))
	for _, file := range files {
		sizeKB := file.SizeBytes / 1024
		if sizeKB <= 0 {
			sizeKB = 1
		}
		extension := strings.TrimSpace(strings.TrimPrefix(file.Extension, "."))
		if extension == "" {
			extension = "unknown"
		}
		archiveSample := make([]string, 0, len(file.ArchiveEntries))
		archiveTruncated := false
		for idx, entry := range file.ArchiveEntries {
			if idx >= 5 {
				archiveTruncated = true
				break
			}
			archiveSample = append(archiveSample, filepath.ToSlash(strings.TrimSpace(entry.Path)))
		}
		result = append(result, promptFile{
			Name:       file.BaseName + file.Extension,
			Folder:     file.RelativePath,
			Extension:  extension,
			MIMEType:   shortMime(file.MIMEType),
			SizeKB:     sizeKB,
			Archive:    archiveSample,
			ArchiveToo: archiveTruncated,
			Metadata:   file.ImageMetadata,
		})
	}
	return result
}

func shortMime(mime string) string {
	trimmed := strings.TrimSpace(mime)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= 40 {
		return trimmed
	}
	return trimmed[:37] + "..."
}

// 3) Verify (+ optional refine)
func (t *Task) Verify(ctx context.Context, gathered pipeline.GatherOutput, response pipeline.LLMResponse) (bool, pipeline.VerifiedOutput, *pipeline.RefineRequest, error) {
	files := gathered.([]FileMeta)
	trimmedRaw := strings.TrimSpace(response.RawText)
	if trimmedRaw == "" {
		return false, nil, &pipeline.RefineRequest{
			UserPromptDelta: fmt.Sprintf("You returned an empty response. Provide a JSON object with key %s containing one entry per file.", sortedFilesKey),
			Reason:          "empty-response",
		}, nil
	}
	parsed, decodeErr := decodeSortedResults(trimmedRaw)
	if decodeErr != nil {
		if errors.Is(decodeErr, errMissingSortedFilesKey) {
			return false, nil, &pipeline.RefineRequest{
				UserPromptDelta: fmt.Sprintf("Respond with a JSON object containing the %s array as described.", sortedFilesKey),
				Reason:          "missing-container",
			}, nil
		}
		return false, nil, &pipeline.RefineRequest{
			UserPromptDelta: fmt.Sprintf("The previous output was not valid JSON for key %s. Return valid JSON only.", sortedFilesKey),
			Reason:          "invalid-json",
		}, nil
	}
	t.lastResponse = response

	if len(parsed) != len(files) {
		return false, nil, &pipeline.RefineRequest{
			UserPromptDelta: fmt.Sprintf("You returned %d items for %d files. Return exactly one classification per file inside %s, ordered.", len(parsed), len(files), sortedFilesKey),
			Reason:          "count-mismatch",
		}, nil
	}

	projectNamePattern := regexp.MustCompile(`^[\w\- ]{2,64}$`)

	var actions []MoveAction
	for idx, item := range parsed {
		file := files[idx]

		trimmedFile := strings.TrimSpace(item.FileName)
		if trimmedFile == "" {
			return false, nil, &pipeline.RefineRequest{
				UserPromptDelta: "Each object must include file_name copied from the metadata.",
				Reason:          "missing-file-name",
			}, nil
		}
		expectedName := file.BaseName + file.Extension
		if trimmedFile != expectedName {
			return false, nil, &pipeline.RefineRequest{
				UserPromptDelta: fmt.Sprintf("file_name must exactly match %q.", expectedName),
				Reason:          "bad-file-name",
			}, nil
		}

		trimmedProject := strings.TrimSpace(item.ProjectName)
		if trimmedProject == "" {
			return false, nil, &pipeline.RefineRequest{
				UserPromptDelta: "Each object must include project_name (2-64 letters/numbers/space/dash/underscore).",
				Reason:          "missing-project-name",
			}, nil
		}
		if !projectNamePattern.MatchString(trimmedProject) {
			return false, nil, &pipeline.RefineRequest{
				UserPromptDelta: "project_name must use letters, numbers, spaces, dashes, or underscores (2-64 chars).",
				Reason:          "bad-project-name",
			}, nil
		}

		trimmedTarget := strings.TrimSpace(item.TargetSubdir)
		if trimmedTarget == "" {
			return false, nil, &pipeline.RefineRequest{
				UserPromptDelta: "Each object must include target_subdir for the destination. Provide a folder path.",
				Reason:          "missing-target",
			}, nil
		}
		targetDir := safeSegment(trimmedTarget)
		if targetDir == "" {
			return false, nil, &pipeline.RefineRequest{
				UserPromptDelta: "Destination path invalid. Use alphanumeric segments separated by '/'.",
				Reason:          "bad-target",
			}, nil
		}

		to := t.fs.FS.Join(t.currentStagingRoot(), targetDir, expectedName)
		actions = append(actions, MoveAction{
			FromPath:    file.AbsolutePath,
			ToPath:      to,
			FileName:    expectedName,
			ProjectName: trimmedProject,
		})
	}
	plan := MovePlan{Actions: actions, DryRun: t.currentDryRun}
	t.Plan = plan
	return true, plan, nil, nil
}

// 4) Apply
func (t *Task) Apply(ctx context.Context, verified pipeline.VerifiedOutput) (pipeline.ApplyReport, error) {
	plan := verified.(MovePlan)
	return t.applyMovePlan(plan)
}

func decodeSortedResults(raw string) ([]LLMResult, error) {
	var objectCandidate map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &objectCandidate); err == nil {
		payload, ok := objectCandidate[sortedFilesKey]
		if !ok {
			return nil, errMissingSortedFilesKey
		}
		var parsed []LLMResult
		if err := json.Unmarshal(payload, &parsed); err != nil {
			return nil, fmt.Errorf("decode %s payload: %w", sortedFilesKey, err)
		}
		return parsed, nil
	}
	var fallback []LLMResult
	if err := json.Unmarshal([]byte(raw), &fallback); err != nil {
		return nil, fmt.Errorf("decode fallback array: %w", err)
	}
	return fallback, nil
}

// --- local helpers ---

func (t *Task) loadProjectListJSON() string {
	cfg, _ := t.cfgProv.Load()
	type Project struct {
		Name     string   `json:"name"`
		Target   string   `json:"target"`
		Keywords []string `json:"keywords"`
	}
	var arr []Project
	for _, p := range cfg.Projects {
		arr = append(arr, Project{Name: p.Name, Target: p.Target, Keywords: p.Keywords})
	}
	b, _ := json.Marshal(arr)
	return string(b)
}

func safeSegment(path string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9 _\-]`)
	segments := strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' })
	var sanitized []string
	for _, segment := range segments {
		trimmed := strings.TrimSpace(segment)
		if trimmed == "" {
			continue
		}
		cleaned := re.ReplaceAllString(trimmed, "_")
		cleaned = strings.Trim(cleaned, " _-")
		if cleaned == "" {
			continue
		}
		sanitized = append(sanitized, cleaned)
	}
	if len(sanitized) == 0 {
		return "Unsorted_Inbox"
	}
	return filepath.Join(sanitized...)
}

func displayRelativePath(root string, absolute string) string {
	relative, err := filepath.Rel(root, absolute)
	if err != nil {
		return ""
	}
	if relative == "." {
		return ""
	}
	return filepath.ToSlash(relative)
}

func (t *Task) currentDownloadsRoot() string {
	if t.downloadsRoot != "" {
		return t.downloadsRoot
	}
	return ""
}

func (t *Task) currentStagingRoot() string {
	if t.stagingRoot != "" {
		return t.stagingRoot
	}
	return ""
}

// --- Config provider abstraction ---

type SortConfigProvider interface {
	Load() (config.Sort, error)
}

type FileSortConfigProvider struct {
	Path string
}

func (p FileSortConfigProvider) Load() (config.Sort, error) {
	sortConfiguration, loadError := config.LoadSort(p.Path)
	if loadError != nil {
		return config.Sort{}, loadError
	}
	return resolveSortGrantBaseDirectories(sortConfiguration)
}
