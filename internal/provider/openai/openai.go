package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"qiservice/internal/provider"
	"strings"
)

type OpenAIProvider struct {
	BaseURL string
}

func NewOpenAIProvider(baseURL string) *OpenAIProvider {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	// Ensure no trailing slash for consistency
	baseURL = strings.TrimRight(baseURL, "/")
	return &OpenAIProvider{
		BaseURL: baseURL,
	}
}

func (p *OpenAIProvider) ChatCompletion(ctx context.Context, req provider.ChatCompletionRequest, apiKey string) (*provider.ChatCompletionResponse, error) {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/chat/completions", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai API error: %d - %s", resp.StatusCode, string(bodyBytes))
	}

	var chatResp provider.ChatCompletionResponse
	bodyBytes, _ := io.ReadAll(resp.Body)

	// Check if it's an SSE stream (starts with "data:")
	if strings.HasPrefix(strings.TrimSpace(string(bodyBytes)), "data:") {
		return p.parseStreamResponse(bodyBytes, req.Model)
	}

	if err := json.Unmarshal(bodyBytes, &chatResp); err != nil {
		// Attempt to grab first 100 chars of body to show what we received
		preview := string(bodyBytes)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		return nil, fmt.Errorf("failed to decode openai response: %v. Response body: %s", err, preview)
	}

	return &chatResp, nil
}

func (p *OpenAIProvider) StreamChatCompletion(ctx context.Context, req provider.ChatCompletionRequest, apiKey string, outputChan chan<- provider.StreamResponse) error {
	req.Stream = true
	reqBody, err := json.Marshal(req)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/chat/completions", bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("openai stream error: %d - %s", resp.StatusCode, string(bodyBytes))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		if !strings.HasPrefix(line, "data:") {
			continue
		}

		dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if dataStr == "[DONE]" {
			break
		}

		var chunk provider.StreamResponse
		if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
			continue
		}

		outputChan <- chunk
	}

	return nil
}

func (p *OpenAIProvider) parseStreamResponse(body []byte, model string) (*provider.ChatCompletionResponse, error) {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	fullContent := ""
	var lastID string
	var finishReason string = "stop"

	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		dataStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if dataStr == "[DONE]" {
			break
		}

		var chunk provider.StreamResponse
		if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
			continue // Skip bad chunks
		}

		if len(chunk.Choices) > 0 {
			fullContent += chunk.Choices[0].Delta.Content
			if chunk.Choices[0].FinishReason != nil {
				finishReason = *chunk.Choices[0].FinishReason
			}
			if chunk.ID != "" {
				lastID = chunk.ID
			}
		}
	}

	// Construct a synthetic single response
	return &provider.ChatCompletionResponse{
		ID:      lastID,
		Object:  "chat.completion",
		Created: 0,
		Model:   model,
		Choices: []provider.Choice{
			{
				Index: 0,
				Message: provider.Message{
					Role:    "assistant",
					Content: fullContent,
				},
				FinishReason: finishReason,
			},
		},
	}, nil
}
