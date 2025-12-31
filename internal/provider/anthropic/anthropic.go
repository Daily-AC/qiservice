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
	Tools     []AnthropicTool    `json:"tools,omitempty"`
}

type AnthropicTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema interface{} `json:"input_schema"`
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

		if msg.Role == "user" || msg.Role == "assistant" {
			// Check for Tool Calls in Assistant message
			if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
				var contentBlocks []map[string]interface{}

				// Add text content if present
				if msg.Content != "" {
					contentBlocks = append(contentBlocks, map[string]interface{}{
						"type": "text",
						"text": msg.Content,
					})
				}

				// Add tool_use blocks
				for _, tc := range msg.ToolCalls {
					var inputMap map[string]interface{}
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &inputMap); err != nil {
						// Fallback if args are not valid JSON (should rare)
						inputMap = map[string]interface{}{}
					}

					contentBlocks = append(contentBlocks, map[string]interface{}{
						"type":  "tool_use",
						"id":    tc.ID,
						"name":  tc.Function.Name,
						"input": inputMap,
					})
				}

				anthropicReq.Messages = append(anthropicReq.Messages, AnthropicMessage{
					Role:    "assistant",
					Content: contentBlocks,
				})
			} else {
				// Standard Text Message
				anthropicReq.Messages = append(anthropicReq.Messages, AnthropicMessage{
					Role:    msg.Role,
					Content: msg.Content,
				})
			}
		} else if msg.Role == "tool" {
			// Convert Tool Result (Role: Tool) -> User Message with tool_result block
			toolResultBlock := map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": msg.ToolCallID,
				"content":     msg.Content,
			}

			// Check if we should merge with previous USER message
			// (Anthropic expects alternating roles usually, but consecutive tools results belong to the same User turn)
			lastIdx := len(anthropicReq.Messages) - 1
			if lastIdx >= 0 && anthropicReq.Messages[lastIdx].Role == "user" {
				// Append to existing User message
				prevContent := anthropicReq.Messages[lastIdx].Content

				var newContent []interface{}
				// Convert previous content to slice if it was string
				if s, ok := prevContent.(string); ok {
					newContent = append(newContent, map[string]interface{}{"type": "text", "text": s})
				} else if list, ok := prevContent.([]interface{}); ok {
					newContent = list
				} else if list, ok := prevContent.([]map[string]interface{}); ok {
					// Handle []map[string]interface{} specifically
					for _, item := range list {
						newContent = append(newContent, item)
					}
				}

				newContent = append(newContent, toolResultBlock)
				anthropicReq.Messages[lastIdx].Content = newContent
			} else {
				// New User Message
				anthropicReq.Messages = append(anthropicReq.Messages, AnthropicMessage{
					Role:    "user",
					Content: []interface{}{toolResultBlock},
				})
			}
		}
	}

	// Map Tools
	if len(req.Tools) > 0 {
		anthropicReq.Tools = []AnthropicTool{}
		for _, t := range req.Tools {
			anthropicReq.Tools = append(anthropicReq.Tools, AnthropicTool{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				InputSchema: t.Function.Parameters,
			})
		}
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
	Type         string          `json:"type"`
	Delta        *AnthropicDelta `json:"delta,omitempty"`
	ContentBlock *AnthropicBlock `json:"content_block,omitempty"`
	Index        int             `json:"index,omitempty"`
}

type AnthropicBlock struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	Text string `json:"text,omitempty"`
}

type AnthropicDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
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

		if msg.Role == "user" || msg.Role == "assistant" {
			// Check for Tool Calls in Assistant message
			if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
				var contentBlocks []map[string]interface{}

				if msg.Content != "" {
					contentBlocks = append(contentBlocks, map[string]interface{}{"type": "text", "text": msg.Content})
				}

				for _, tc := range msg.ToolCalls {
					var inputMap map[string]interface{}
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &inputMap); err != nil {
						inputMap = map[string]interface{}{}
					}
					contentBlocks = append(contentBlocks, map[string]interface{}{
						"type":  "tool_use",
						"id":    tc.ID,
						"name":  tc.Function.Name,
						"input": inputMap,
					})
				}

				anthropicReq.Messages = append(anthropicReq.Messages, AnthropicMessage{Role: "assistant", Content: contentBlocks})
			} else {
				anthropicReq.Messages = append(anthropicReq.Messages, AnthropicMessage{Role: msg.Role, Content: msg.Content})
			}
		} else if msg.Role == "tool" {
			toolResultBlock := map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": msg.ToolCallID,
				"content":     msg.Content,
			}

			lastIdx := len(anthropicReq.Messages) - 1
			if lastIdx >= 0 && anthropicReq.Messages[lastIdx].Role == "user" {
				prevContent := anthropicReq.Messages[lastIdx].Content
				var newContent []interface{}
				if s, ok := prevContent.(string); ok {
					newContent = append(newContent, map[string]interface{}{"type": "text", "text": s})
				} else if list, ok := prevContent.([]interface{}); ok {
					newContent = list
				} else if list, ok := prevContent.([]map[string]interface{}); ok {
					for _, item := range list {
						newContent = append(newContent, item)
					}
				}
				newContent = append(newContent, toolResultBlock)
				anthropicReq.Messages[lastIdx].Content = newContent
			} else {
				anthropicReq.Messages = append(anthropicReq.Messages, AnthropicMessage{Role: "user", Content: []interface{}{toolResultBlock}})
			}
		}
	}

	// Map Tools
	if len(req.Tools) > 0 {
		anthropicReq.Tools = []AnthropicTool{}
		for _, t := range req.Tools {
			var schema interface{}
			// Ensure schema is properly set (handle any/map)
			schema = t.Function.Parameters

			anthropicReq.Tools = append(anthropicReq.Tools, AnthropicTool{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				InputSchema: schema,
			})
		}
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
				Choices: []provider.StreamChoice{{Index: 0, Delta: provider.Message{Role: "assistant"}}},
			}
		} else if event.Type == "content_block_start" {
			// Tool Use Start
			if event.ContentBlock != nil && event.ContentBlock.Type == "tool_use" {
				outputChan <- provider.StreamResponse{
					ID:      "chatcmpl-stream",
					Object:  "chat.completion.chunk",
					Created: time.Now().Unix(),
					Model:   req.Model,
					Choices: []provider.StreamChoice{{
						Index: 0,
						Delta: provider.Message{
							ToolCalls: []provider.ToolCall{{
								ID:   event.ContentBlock.ID,
								Type: "function",
								Function: provider.FunctionCall{
									Name: event.ContentBlock.Name,
								},
							}},
						},
					}},
				}
			}
		} else if event.Type == "content_block_delta" {
			if event.Delta != nil {
				if event.Delta.Type == "text_delta" {
					// Text Content
					outputChan <- provider.StreamResponse{
						ID:      "chatcmpl-stream",
						Object:  "chat.completion.chunk",
						Created: time.Now().Unix(),
						Model:   req.Model,
						Choices: []provider.StreamChoice{{Index: 0, Delta: provider.Message{Content: event.Delta.Text}}},
					}
				} else if event.Delta.Type == "input_json_delta" {
					// Tool Arguments
					outputChan <- provider.StreamResponse{
						ID:      "chatcmpl-stream",
						Object:  "chat.completion.chunk",
						Created: time.Now().Unix(),
						Model:   req.Model,
						Choices: []provider.StreamChoice{{
							Index: 0,
							Delta: provider.Message{
								ToolCalls: []provider.ToolCall{{
									Function: provider.FunctionCall{
										Arguments: event.Delta.PartialJSON,
									},
								}},
							},
						}},
					}
				}
			}
		}
	}

	return nil
}
