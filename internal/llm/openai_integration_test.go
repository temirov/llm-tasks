package llm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCreateChatCompletionIntegration(t *testing.T) {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		t.Fatal("OPENAI_API_KEY must be set for integration tests")
	}

	model := strings.TrimSpace(os.Getenv("LLM_TASKS_INTEGRATION_MODEL"))
	if model == "" {
		model = "gpt-4o-mini"
	}

	client := Client{
		HTTPBaseURL: "https://api.openai.com/v1",
		APIKey:      apiKey,
	}

	zeroTemperature := 0.0
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	response, err := client.CreateChatCompletion(ctx, ChatCompletionRequest{
		Model: model,
		Messages: []ChatMessage{
			{Role: "system", Content: "You respond with the single word pong."},
			{Role: "user", Content: "ping"},
		},
		MaxCompletionTokens: 16,
		Temperature:         &zeroTemperature,
	})
	if err != nil {
		t.Fatalf("CreateChatCompletion integration call failed: %v", err)
	}
	if !strings.Contains(strings.ToLower(response), "pong") {
		t.Fatalf("expected response to mention pong, got %q", response)
	}
}
