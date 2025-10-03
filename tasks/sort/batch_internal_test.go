package sort

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/temirov/llm-tasks/internal/config"
	"github.com/temirov/llm-tasks/internal/fsops"
	"github.com/temirov/llm-tasks/internal/pipeline"
)

type stubLLM struct {
	tokens []int
}

func (s *stubLLM) Chat(ctx context.Context, req pipeline.LLMRequest) (pipeline.LLMResponse, error) {
	s.tokens = append(s.tokens, req.MaxTokens)
	if req.MaxTokens <= 512 {
		return pipeline.LLMResponse{}, fmt.Errorf(`chat completion returned empty message (status=200 body={"choices":[{"finish_reason": "length"}]})`)
	}
	responses := []LLMResult{
		{
			FileName:     "code.txt",
			ProjectName:  "Project",
			TargetSubdir: "Projects/Codebases",
		},
	}
	envelope := map[string][]LLMResult{
		sortedFilesKey: responses,
	}
	raw, _ := json.Marshal(envelope)
	return pipeline.LLMResponse{RawText: string(raw)}, nil
}

type stubSortProvider struct{ cfg config.Sort }

func (s stubSortProvider) Load() (config.Sort, error) { return s.cfg, nil }

func TestRunBatchesRetriesOnLength(t *testing.T) {
	mem := fsops.NewMem()
	downloads := "/downloads"
	staging := "/staging"
	if err := mem.MkdirAll(downloads, 0o755); err != nil {
		t.Fatalf("mkdir downloads: %v", err)
	}
	if err := mem.WriteFile(downloads+"/code.txt", []byte("data"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg := config.Sort{}
	cfg.Grant.BaseDirectories.Downloads = downloads
	cfg.Grant.BaseDirectories.Staging = staging
	cfg.Grant.Safety.DryRun = true
	cfg.Projects = append(cfg.Projects, struct {
		Name     string   `yaml:"name"`
		Target   string   `yaml:"target"`
		Keywords []string `yaml:"keywords"`
	}{Name: "Code", Target: "Projects/Codebases", Keywords: []string{"code"}})

	provider := stubSortProvider{cfg: cfg}
	task := NewWithDeps(fsops.NewOps(mem), provider).(*Task)
	task.SetCompletionTokens(512)

	llmClient := &stubLLM{}
	runner := pipeline.Runner{
		Client: llmClient,
		Options: pipeline.RunOptions{
			MaxAttempts: 1,
			DryRun:      true,
			Timeout:     5 * time.Second,
		},
	}

	report, err := RunBatches(context.Background(), runner, task, 1)
	if err != nil {
		t.Fatalf("RunBatches: %v", err)
	}
	if len(llmClient.tokens) < 2 {
		t.Fatalf("expected fallback retries, got %v", llmClient.tokens)
	}
	if llmClient.tokens[0] >= llmClient.tokens[1] {
		t.Fatalf("expected increased token budget on retry, got %v", llmClient.tokens)
	}
	if report.NumActions != 1 {
		t.Fatalf("expected 1 action, got %d", report.NumActions)
	}
}
