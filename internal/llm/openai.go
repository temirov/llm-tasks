package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	Model               string        `json:"model"`
	Messages            []ChatMessage `json:"messages"`
	MaxCompletionTokens int           `json:"max_completion_tokens,omitempty"` // OpenAI 2025 param name
	Temperature         float64       `json:"temperature,omitempty"`
}

type ChatCompletionResponse struct {
	Choices []struct {
		Message ChatMessage `json:"message"`
	} `json:"choices"`
}

func (c Client) CreateChatCompletion(ctx context.Context, requestPayload ChatCompletionRequest) (string, error) {
	requestBytes, marshalErr := json.Marshal(requestPayload)
	if marshalErr != nil {
		return "", marshalErr
	}

	req, buildErr := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.HTTPBaseURL+"/chat/completions",
		bytes.NewReader(requestBytes),
	)
	if buildErr != nil {
		return "", buildErr
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	httpClient := &http.Client{}
	res, httpErr := httpClient.Do(req)
	if httpErr != nil {
		return "", httpErr
	}
	defer func(closer io.ReadCloser) { _ = closer.Close() }(res.Body)

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("llm http error %d: %s", res.StatusCode, string(bodyBytes))
	}

	var completion ChatCompletionResponse
	if err := json.NewDecoder(res.Body).Decode(&completion); err != nil {
		return "", err
	}
	if len(completion.Choices) == 0 {
		return "", fmt.Errorf("empty completion")
	}
	return completion.Choices[0].Message.Content, nil
}
