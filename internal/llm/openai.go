package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Client struct {
	HTTPBaseURL       string
	APIKey            string
	ModelIdentifier   string
	MaxTokensResponse int
	Temperature       float64
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompletionRequest struct {
	Model               string          `json:"model"`
	Messages            []ChatMessage   `json:"messages"`
	MaxCompletionTokens int             `json:"max_completion_tokens,omitempty"`
	Temperature         *float64        `json:"temperature,omitempty"`
	ResponseFormat      *responseFormat `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type       string             `json:"type"`
	JSONSchema *jsonSchemaWrapper `json:"json_schema,omitempty"`
}

type jsonSchemaWrapper struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
	Strict bool            `json:"strict,omitempty"`
}

type chatMessageResponse struct {
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Refusal   json.RawMessage `json:"refusal,omitempty"`
	ToolCalls json.RawMessage `json:"tool_calls,omitempty"`
}

type chatCompletionChoice struct {
	Message      chatMessageResponse `json:"message"`
	FinishReason string              `json:"finish_reason"`
}

type ChatCompletionResponse struct {
	Choices []chatCompletionChoice `json:"choices"`
}

func truncateForLog(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}

func (c Client) CreateChatCompletion(ctx context.Context, requestPayload ChatCompletionRequest) (string, error) {
	requestBytes, marshalErr := json.Marshal(requestPayload)
	if marshalErr != nil {
		return "", marshalErr
	}
	httpRequest, buildErr := http.NewRequestWithContext(ctx, http.MethodPost, c.HTTPBaseURL+"/chat/completions", bytes.NewReader(requestBytes))
	if buildErr != nil {
		return "", buildErr
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+c.APIKey)

	httpClient := &http.Client{}
	httpResponse, httpErr := httpClient.Do(httpRequest)
	if httpErr != nil {
		return "", httpErr
	}
	defer func(closer io.ReadCloser) { _ = closer.Close() }(httpResponse.Body)

	bodyBytes, readErr := io.ReadAll(httpResponse.Body)
	if readErr != nil {
		return "", readErr
	}
	bodyPreview := truncateForLog(string(bodyBytes), 512)

	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		return "", fmt.Errorf("llm http error %d: %s", httpResponse.StatusCode, bodyPreview)
	}

	var completion ChatCompletionResponse
	if decodeErr := json.Unmarshal(bodyBytes, &completion); decodeErr != nil {
		return "", fmt.Errorf("decode chat completion: %w (body=%s)", decodeErr, bodyPreview)
	}
	if len(completion.Choices) == 0 {
		return "", fmt.Errorf("chat completion returned no choices (status=%d body=%s)", httpResponse.StatusCode, bodyPreview)
	}

	choice := completion.Choices[0]
	content, extractErr := extractMessageContent(choice.Message)
	if extractErr != nil {
		return "", fmt.Errorf("chat completion parse error: %w (body=%s)", extractErr, bodyPreview)
	}

	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		if strings.EqualFold(strings.TrimSpace(choice.FinishReason), "length") {
			return "", fmt.Errorf("chat completion returned empty message (status=%d body=%s)", httpResponse.StatusCode, bodyPreview)
		}
		if refusal := decodeRefusal(choice.Message.Refusal); refusal != "" {
			return "", fmt.Errorf("chat completion refusal: %s (status=%d body=%s)", refusal, httpResponse.StatusCode, bodyPreview)
		}
		return "", fmt.Errorf("chat completion returned empty message (status=%d body=%s)", httpResponse.StatusCode, bodyPreview)
	}
	return trimmed, nil
}

func extractMessageContent(message chatMessageResponse) (string, error) {
	if len(message.Content) == 0 || string(message.Content) == "null" {
		refusal := decodeRefusal(message.Refusal)
		if refusal != "" {
			return "", fmt.Errorf("chat completion refusal: %s", refusal)
		}
		return "", nil
	}

	var asString string
	if err := json.Unmarshal(message.Content, &asString); err == nil {
		return asString, nil
	}

	if text, ok := extractRichText(message.Content); ok {
		return text, nil
	}

	refusal := decodeRefusal(message.Refusal)
	if refusal != "" {
		return "", fmt.Errorf("chat completion refusal: %s", refusal)
	}

	if len(message.ToolCalls) > 0 && string(message.ToolCalls) != "null" {
		return "", fmt.Errorf("chat completion produced tool_calls: %s", truncateForLog(string(message.ToolCalls), 240))
	}

	return "", fmt.Errorf("unsupported message content: %s", truncateForLog(string(message.Content), 240))
}

func extractRichText(raw json.RawMessage) (string, bool) {
	fragments := gatherTextFragments(raw)
	if len(fragments) == 0 {
		return "", false
	}
	combined := strings.TrimSpace(strings.Join(fragments, "\n"))
	if combined == "" {
		return "", false
	}
	return combined, true
}

func gatherTextFragments(raw json.RawMessage) []string {
	var data any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil
	}
	return flattenText(data)
}

func flattenText(value any) []string {
	switch v := value.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return nil
		}
		return []string{trimmed}
	case []any:
		var collected []string
		for _, item := range v {
			collected = append(collected, flattenText(item)...)
		}
		return collected
	case map[string]any:
		if text, ok := v["text"]; ok {
			return flattenText(text)
		}
		if content, ok := v["content"]; ok {
			return flattenText(content)
		}
		if valuePart, ok := v["value"]; ok {
			return flattenText(valuePart)
		}
		var collected []string
		for _, nested := range v {
			collected = append(collected, flattenText(nested)...)
		}
		return collected
	default:
		return nil
	}
}

func decodeRefusal(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var refusalString string
	if err := json.Unmarshal(raw, &refusalString); err == nil {
		return strings.TrimSpace(refusalString)
	}
	if text, ok := extractRichText(raw); ok {
		return text
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err == nil {
		if textValue, ok := generic["text"].(string); ok {
			return strings.TrimSpace(textValue)
		}
	}
	return strings.TrimSpace(truncateForLog(string(raw), 200))
}
