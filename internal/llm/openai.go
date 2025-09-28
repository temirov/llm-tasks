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
	MaxCompletionTokens int           `json:"max_completion_tokens,omitempty"`
	Temperature         *float64      `json:"temperature,omitempty"`
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

	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(httpResponse.Body)
		return "", fmt.Errorf("llm http error %d: %s", httpResponse.StatusCode, string(bodyBytes))
	}

	var completion ChatCompletionResponse
	decodeErr := json.NewDecoder(httpResponse.Body).Decode(&completion)
	if decodeErr != nil {
		return "", decodeErr
	}
	if len(completion.Choices) == 0 {
		return "", fmt.Errorf("empty completion")
	}
	return completion.Choices[0].Message.Content, nil
}
