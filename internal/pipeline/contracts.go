package pipeline

import "context"

// SortedFilesSchemaName describes the canonical identifier for the sort task response schema.
const SortedFilesSchemaName = "sorted_files"

type Pipeline interface {
	Name() string
	Gather(ctx context.Context) (GatherOutput, error)
	Prompt(ctx context.Context, gathered GatherOutput) (LLMRequest, error)
	Verify(ctx context.Context, gathered GatherOutput, response LLMResponse) (accepted bool, verified VerifiedOutput, refine *RefineRequest, err error)
	Apply(ctx context.Context, verified VerifiedOutput) (ApplyReport, error)
}

type GatherOutput any
type VerifiedOutput any

type LLMRequest struct {
	SystemPrompt string
	UserPrompt   string
	JSONSchema   []byte
	MaxTokens    int
	Temperature  float64
	Model        string
}

type LLMResponse struct {
	RawText string
}

type RefineRequest struct {
	UserPromptDelta string
	Reason          string
}

type ApplyReport struct {
	DryRun     bool
	Summary    string
	NumActions int
}
