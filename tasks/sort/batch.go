package sort

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/temirov/llm-tasks/internal/pipeline"
)

const DefaultBatchSize = 1

func RunBatches(ctx context.Context, runner pipeline.Runner, prototype *Task, batchSize int) (pipeline.ApplyReport, error) {
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	inventoryTask := prototype.Clone()
	output, err := inventoryTask.Gather(ctx)
	if err != nil {
		return pipeline.ApplyReport{}, fmt.Errorf("gather inventory: %w", err)
	}
	files := output.([]FileMeta)
	batches := chunkFileMetas(files, batchSize)
	defaults, cfgErr := prototype.cfgProv.Load()
	if cfgErr != nil {
		return pipeline.ApplyReport{}, fmt.Errorf("load sort config: %w", cfgErr)
	}

	fallbacks := []int{768, 1024, 1280, 1536, 1792}
	totalActions := 0
	dryRun := true
	sawBatch := false
	for index, batch := range batches {
		if len(batch) == 0 {
			continue
		}
		report, runErr := processBatch(ctx, runner, prototype, batch, fallbacks)
		if runErr != nil {
			return pipeline.ApplyReport{}, fmt.Errorf("batch %d: %w", index+1, runErr)
		}
		totalActions += report.NumActions
		dryRun = dryRun && report.DryRun
		sawBatch = true
	}
	if !sawBatch {
		dryRun = defaults.Grant.Safety.DryRun
	}

	summary := fmt.Sprintf("sort: %d actions across %d batches", totalActions, len(batches))
	return pipeline.ApplyReport{
		DryRun:     dryRun,
		Summary:    summary,
		NumActions: totalActions,
	}, nil
}

func chunkFileMetas(files []FileMeta, size int) [][]FileMeta {
	if len(files) == 0 {
		return [][]FileMeta{}
	}
	if size <= 0 {
		size = 1
	}
	var batches [][]FileMeta
	for start := 0; start < len(files); start += size {
		end := start + size
		if end > len(files) {
			end = len(files)
		}
		batch := append([]FileMeta(nil), files[start:end]...)
		batches = append(batches, batch)
	}
	return batches
}

func ChunkFileMetasForTest(files []FileMeta, size int) [][]FileMeta {
	return chunkFileMetas(files, size)
}

func processBatch(ctx context.Context, runner pipeline.Runner, prototype *Task, batch []FileMeta, fallbacks []int) (pipeline.ApplyReport, error) {
	if len(batch) == 0 {
		return pipeline.ApplyReport{}, nil
	}

	maxTokens := prototype.completionTokens
	if maxTokens <= 0 {
		maxTokens = sortCompletionMaxTokens
	}

	task := prototype.Clone()
	task.Preload(batch)
	task.SetCompletionTokens(maxTokens)
	report, err := runner.Run(ctx, task)
	if err == nil {
		return report, nil
	}

	if !isLengthError(err) {
		return pipeline.ApplyReport{}, annotateLLMError(err, "initial", task, batch)
	}

	if len(batch) > 1 {
		mid := len(batch) / 2
		leftReport, leftErr := processBatch(ctx, runner, prototype, batch[:mid], fallbacks)
		if leftErr != nil {
			return pipeline.ApplyReport{}, leftErr
		}
		rightReport, rightErr := processBatch(ctx, runner, prototype, batch[mid:], fallbacks)
		if rightErr != nil {
			return pipeline.ApplyReport{}, rightErr
		}
		merged := mergeReports(leftReport, rightReport)
		if merged.NumActions > 0 {
			return merged, nil
		}
	}

	for _, tokens := range fallbacks {
		fallbackTask := prototype.Clone()
		fallbackTask.Preload(batch)
		fallbackTask.SetCompletionTokens(tokens)
		fallbackReport, fallbackErr := runner.Run(ctx, fallbackTask)
		if fallbackErr == nil {
			return fallbackReport, nil
		}
		if !isLengthError(fallbackErr) {
			return pipeline.ApplyReport{}, annotateLLMError(fallbackErr, fmt.Sprintf("fallback-%d", tokens), fallbackTask, batch)
		}
	}

	return pipeline.ApplyReport{}, annotateLLMError(err, "final", task, batch)
}

func mergeReports(left, right pipeline.ApplyReport) pipeline.ApplyReport {
	return pipeline.ApplyReport{
		DryRun:     left.DryRun && right.DryRun,
		Summary:    fmt.Sprintf("%s; %s", strings.TrimSpace(left.Summary), strings.TrimSpace(right.Summary)),
		NumActions: left.NumActions + right.NumActions,
	}
}

func isLengthError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "finish_reason\": \"length\"") || strings.Contains(msg, "\"finish_reason\": \"length\"") || strings.Contains(msg, "finish_reason\":\"length\"")
}

func annotateLLMError(err error, stage string, task *Task, batch []FileMeta) error {
	info := struct {
		Stage    string       `json:"stage"`
		Request  requestDebug `json:"request"`
		Response string       `json:"response"`
		Files    []promptFile `json:"files"`
	}{
		Stage:    stage,
		Request:  captureRequest(task.lastRequest),
		Response: truncateDebug(task.lastResponse.RawText, 600),
		Files:    buildPromptFiles(batch),
	}
	encoded, _ := json.Marshal(info)
	return fmt.Errorf("%w; context=%s", err, string(encoded))
}

type requestDebug struct {
	Model     string `json:"model"`
	MaxTokens int    `json:"max_tokens"`
	System    string `json:"system_prompt"`
	User      string `json:"user_prompt"`
}

func captureRequest(req pipeline.LLMRequest) requestDebug {
	return requestDebug{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		System:    truncateDebug(req.SystemPrompt, 400),
		User:      truncateDebug(req.UserPrompt, 600),
	}
}

func truncateDebug(text string, limit int) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) <= limit {
		return trimmed
	}
	runes := []rune(trimmed)
	if len(runes) <= limit {
		return trimmed
	}
	return string(runes[:limit]) + "…"
}
