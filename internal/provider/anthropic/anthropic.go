package anthropic

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
	"time"
)

type AnthropicProvider struct {
	BaseURL string
}

func NewAnthropicProvider(baseURL string) *AnthropicProvider {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return &AnthropicProvider{
		BaseURL: baseURL,
	}
}

// Anthropic structures
type AnthropicRequest struct {
	Model     string             `json:"model"`
	Messages  []AnthropicMessage `json:"messages"`
	System    interface{}        `json:"system,omitempty"` // string or []AnthropicContent
	MaxTokens int                `json:"max_tokens"`
	Stream    bool               `json:"stream,omitempty"`
}

type AnthropicMessage struct {
	ID      string      `json:"id,omitempty"`
	Type    string      `json:"type,omitempty"`
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string or []AnthropicContent
	Model   string      `json:"model,omitempty"`
	Usage   *Usage      `json:"usage,omitempty"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type AnthropicResponse struct {
	ID           string             `json:"id"`
	Type         string             `json:"type"`
	Role         string             `json:"role"`
	Content      []AnthropicContent `json:"content"`
	StopReason   *string            `json:"stop_reason"`
	StopSequence *string            `json:"stop_sequence"`
	Usage        *Usage             `json:"usage,omitempty"`
}

type AnthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ExtractText retrieves text from string or []map[string]interface{} (json unmarshal result)
func ExtractText(content interface{}) string {
	if s, ok := content.(string); ok {
		return s
	}

	// Handle []interface{} (from JSON array)
	if list, ok := content.([]interface{}); ok {
		var sb strings.Builder
		for _, item := range list {
			if m, ok := item.(map[string]interface{}); ok {
				if t, ok := m["type"].(string); ok && t == "text" {
					if text, ok := m["text"].(string); ok {
						if sb.Len() > 0 {
							sb.WriteString("\n")
						}
						sb.WriteString(text)
					}
				}
			}
		}
		return sb.String()
	}

	return ""
}

func (p *AnthropicProvider) ChatCompletion(ctx context.Context, req provider.ChatCompletionRequest, apiKey string) (*provider.ChatCompletionResponse, error) {
	anthropicReq := AnthropicRequest{
		Model:     req.Model,
		MaxTokens: 4096, // Default max tokens as Anthropic requires it
		Messages:  []AnthropicMessage{},
	}

	for _, msg := range req.Messages {
		if msg.Role == "system" {
			anthropicReq.System = msg.Content
			continue
		}
		anthropicReq.Messages = append(anthropicReq.Messages, AnthropicMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	reqBody, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/messages", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic API error: %d - %s", resp.StatusCode, string(bodyBytes))
	}

	var anthroResp AnthropicResponse
	bodyBytes, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(bodyBytes, &anthroResp); err != nil {
		preview := string(bodyBytes)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		return nil, fmt.Errorf("failed to decode anthropic response: %v. Response body: %s", err, preview)
	}

	// Map back
	content := ""
	if len(anthroResp.Content) > 0 {
		content = anthroResp.Content[0].Text
	}

	finishReason := "stop"
	if anthroResp.StopReason != nil {
		finishReason = *anthroResp.StopReason
	}

	return &provider.ChatCompletionResponse{
		ID:      anthroResp.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []provider.Choice{
			{
				Index: 0,
				Message: provider.Message{
					Role:    "assistant",
					Content: content,
				},
				FinishReason: finishReason,
			},
		},
	}, nil
}

// Anthropic Streaming Events
type AnthropicEvent struct {
	Type  string          `json:"type"`
	Delta *AnthropicDelta `json:"delta,omitempty"`
}

type AnthropicDelta struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

func (p *AnthropicProvider) StreamChatCompletion(ctx context.Context, req provider.ChatCompletionRequest, apiKey string, outputChan chan<- provider.StreamResponse) error {
	anthropicReq := AnthropicRequest{
		Model:     req.Model,
		MaxTokens: 4096,
		Messages:  []AnthropicMessage{},
		Stream:    true,
	}

	for _, msg := range req.Messages {
		if msg.Role == "system" {
			anthropicReq.System = msg.Content
			continue
		}
		anthropicReq.Messages = append(anthropicReq.Messages, AnthropicMessage{Role: msg.Role, Content: msg.Content})
	}

	reqBody, _ := json.Marshal(anthropicReq)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/messages", bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("anthropic stream error: %d - %s", resp.StatusCode, string(bodyBytes))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		dataStr := strings.TrimPrefix(line, "data: ")

		var event AnthropicEvent
		if err := json.Unmarshal([]byte(dataStr), &event); err != nil {
			continue
		}

		// Handle different Anthropic Events
		if event.Type == "message_start" {
			// First chunk: Send Role
			outputChan <- provider.StreamResponse{
				ID:      "chatcmpl-stream",
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   req.Model,
				Choices: []provider.StreamChoice{
					{
						Index: 0,
						Delta: provider.Message{
							Role: "assistant", // Only Role
						},
					},
				},
			}
		} else if event.Type == "content_block_delta" && event.Delta != nil && event.Delta.Type == "text_delta" {
			// Subsequent chunks: Send Content (No Role)
			outputChan <- provider.StreamResponse{
				ID:      "chatcmpl-stream",
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   req.Model,
				Choices: []provider.StreamChoice{
					{
						Index: 0,
						Delta: provider.Message{
							Content: event.Delta.Text, // Only Content
						},
					},
				},
			}
		}
	}

	return nil
}
