package llm

import (
	"context"
	"strings"

	"github.com/temirov/llm-tasks/internal/pipeline"
)

// Adapter adapts pipeline.LLMRequest to the concrete HTTP client.
type Adapter struct {
	Client        Client
	DefaultModel  string
	DefaultTemp   float64
	DefaultTokens int
}

func (a Adapter) Chat(ctx context.Context, req pipeline.LLMRequest) (pipeline.LLMResponse, error) {
	model := req.Model
	if strings.TrimSpace(model) == "" {
		model = a.DefaultModel
	}

	// Build request
	cr := ChatCompletionRequest{
		Model: model,
		Messages: []ChatMessage{
			{Role: "system", Content: strings.TrimSpace(req.SystemPrompt)},
			{Role: "user", Content: strings.TrimSpace(req.UserPrompt)},
		},
		MaxCompletionTokens: chooseInt(req.MaxTokens, a.DefaultTokens),
	}

	// Many 2025 models only allow the default temperature (1). If the resolved
	// temperature is 0 or 1, we omit it (let server default). If it’s some
	// other value, only include it when it’s not 1.
	resolvedTemp := chooseFloat(req.Temperature, a.DefaultTemp)
	if resolvedTemp != 0 && resolvedTemp != 1 {
		cr.Temperature = resolvedTemp
	}

	out, err := a.Client.CreateChatCompletion(ctx, cr)
	if err != nil {
		return pipeline.LLMResponse{}, err
	}
	return pipeline.LLMResponse{RawText: out}, nil
}

func chooseInt(a, b int) int {
	if a > 0 {
		return a
	}
	return b
}

func chooseFloat(a, b float64) float64 {
	if a > 0 {
		return a
	}
	return b
}
