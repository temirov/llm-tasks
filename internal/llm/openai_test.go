package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateChatCompletionEmptyMessageLengthFinish(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		payload := map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": "",
						"role":    "assistant",
					},
					"finish_reason": "length",
				},
			},
		}
		if err := json.NewEncoder(writer).Encode(payload); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}))
	defer server.Close()

	client := Client{HTTPBaseURL: server.URL, APIKey: "test"}
	result, err := client.CreateChatCompletion(context.Background(), ChatCompletionRequest{Model: "m"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Fatalf("expected empty string, got %q", result)
	}
}

func TestCreateChatCompletionSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		payload := map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": "  result  ",
						"role":    "assistant",
					},
					"finish_reason": "stop",
				},
			},
		}
		if err := json.NewEncoder(writer).Encode(payload); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}))
	defer server.Close()

	client := Client{HTTPBaseURL: server.URL, APIKey: "test"}
	result, err := client.CreateChatCompletion(context.Background(), ChatCompletionRequest{Model: "m"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "result" {
		t.Fatalf("expected result trimmed, got %q", result)
	}
}

func TestCreateChatCompletionStructuredContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		payload := map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": []any{
							map[string]any{
								"type": "output_text",
								"text": []any{
									map[string]any{
										"type": "text",
										"text": "alpha",
									},
								},
							},
							map[string]any{
								"type": "output_text",
								"text": "beta",
							},
						},
						"role": "assistant",
					},
					"finish_reason": "length",
				},
			},
		}
		if err := json.NewEncoder(writer).Encode(payload); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}))
	defer server.Close()

	client := Client{HTTPBaseURL: server.URL, APIKey: "test"}
	result, err := client.CreateChatCompletion(context.Background(), ChatCompletionRequest{Model: "m"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "alpha\nbeta" {
		t.Fatalf("expected flattened text, got %q", result)
	}
}
