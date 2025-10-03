package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/temirov/llm-tasks/internal/pipeline"
)

func TestAdapterSetsJSONSchemaResponseFormat(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		payload := map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": "[{\"file_name\":\"a.txt\",\"project_name\":\"Demo\",\"target_subdir\":\"Projects/Demo\"}]",
						"role":    "assistant",
					},
					"finish_reason": "stop",
				},
			},
		}
		if err := json.NewEncoder(writer).Encode(payload); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	adapter := Adapter{
		Client: Client{HTTPBaseURL: server.URL, APIKey: "test"},
	}

	schema := []byte(fmt.Sprintf(`{"type":"object","properties":{"%s":{"type":"array","items":{"type":"object"}}},"required":["%s"],"additionalProperties":false}`, pipeline.SortedFilesSchemaName, pipeline.SortedFilesSchemaName))
	resp, err := adapter.Chat(context.Background(), pipeline.LLMRequest{
		SystemPrompt: "system",
		UserPrompt:   "user",
		MaxTokens:    128,
		JSONSchema:   schema,
	})
	if err != nil {
		t.Fatalf("adapter chat: %v", err)
	}
	if resp.RawText == "" {
		t.Fatalf("expected non-empty response")
	}

	rf, ok := received["response_format"].(map[string]any)
	if !ok {
		t.Fatalf("expected response_format in request, got %v", received["response_format"])
	}
	if rf["type"] != "json_schema" {
		t.Fatalf("expected type json_schema, got %v", rf["type"])
	}
	schemaPayload, ok := rf["json_schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected json_schema payload, got %v", rf["json_schema"])
	}
	if schemaPayload["name"] != pipeline.SortedFilesSchemaName {
		t.Fatalf("unexpected schema name: %v", schemaPayload["name"])
	}
}
