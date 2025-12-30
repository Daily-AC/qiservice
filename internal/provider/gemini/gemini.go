package gemini

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

type GeminiProvider struct {
	BaseURL string
}

func NewGeminiProvider(baseURL string) *GeminiProvider {
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta/models"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return &GeminiProvider{
		BaseURL: baseURL,
	}
}

// Gemini structures
type GeminiRequest struct {
	Contents          []GeminiContent `json:"contents"`
	SystemInstruction *GeminiContent  `json:"system_instruction,omitempty"`
}

type GeminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []GeminiPart `json:"parts"`
}

type GeminiPart struct {
	Text string `json:"text"`
}

type GeminiResponse struct {
	Candidates []GeminiCandidate `json:"candidates"`
}

type GeminiCandidate struct {
	Content      GeminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
	Index        int           `json:"index"`
}

func (p *GeminiProvider) ChatCompletion(ctx context.Context, req provider.ChatCompletionRequest, apiKey string) (*provider.ChatCompletionResponse, error) {
	geminiReq := GeminiRequest{
		Contents: []GeminiContent{},
	}

	for _, msg := range req.Messages {
		if msg.Role == "system" {
			geminiReq.SystemInstruction = &GeminiContent{
				Parts: []GeminiPart{{Text: msg.Content}},
			}
			continue
		}

		role := "user"
		if msg.Role == "assistant" {
			role = "model"
		}

		geminiReq.Contents = append(geminiReq.Contents, GeminiContent{
			Role:  role,
			Parts: []GeminiPart{{Text: msg.Content}},
		})
	}

	reqBody, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/%s:generateContent?key=%s", p.BaseURL, req.Model, apiKey)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini API error: %d - %s", resp.StatusCode, string(bodyBytes))
	}

	var geminiResp GeminiResponse
	bodyBytes, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(bodyBytes, &geminiResp); err != nil {
		preview := string(bodyBytes)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		return nil, fmt.Errorf("failed to decode gemini response: %v. Response body: %s", err, preview)
	}

	// Map back to OpenAI format
	choices := []provider.Choice{}
	for _, candidate := range geminiResp.Candidates {
		content := ""
		if len(candidate.Content.Parts) > 0 {
			content = candidate.Content.Parts[0].Text
		}

		choices = append(choices, provider.Choice{
			Index: candidate.Index,
			Message: provider.Message{
				Role:    "assistant",
				Content: content,
			},
			FinishReason: candidate.FinishReason, // Note: Might need mapping standard values (STOP -> stop)
		})
	}

	return &provider.ChatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().Unix()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: choices,
	}, nil
}

func (p *GeminiProvider) StreamChatCompletion(ctx context.Context, req provider.ChatCompletionRequest, apiKey string, outputChan chan<- provider.StreamResponse) error {
	// Prepare Gemini Request
	geminiReq := GeminiRequest{
		Contents: []GeminiContent{},
	}
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			geminiReq.SystemInstruction = &GeminiContent{Parts: []GeminiPart{{Text: msg.Content}}}
			continue
		}
		role := "user"
		if msg.Role == "assistant" {
			role = "model"
		}
		geminiReq.Contents = append(geminiReq.Contents, GeminiContent{Role: role, Parts: []GeminiPart{{Text: msg.Content}}})
	}

	reqBody, _ := json.Marshal(geminiReq)
	url := fmt.Sprintf("%s/%s:streamGenerateContent?key=%s&alt=sse", p.BaseURL, req.Model, apiKey) // Use alt=sse for easier parsing

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gemini stream error: %d - %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse SSE from Gemini (alt=sse returns standard SSE)
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		dataStr := strings.TrimPrefix(line, "data: ")

		var geminiResp GeminiResponse
		if err := json.Unmarshal([]byte(dataStr), &geminiResp); err != nil {
			continue
		}

		if len(geminiResp.Candidates) > 0 {
			content := ""
			if len(geminiResp.Candidates[0].Content.Parts) > 0 {
				content = geminiResp.Candidates[0].Content.Parts[0].Text
			}

			// Send Chunk
			outputChan <- provider.StreamResponse{
				ID:      fmt.Sprintf("chatcmpl-%d", time.Now().Unix()),
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   req.Model,
				Choices: []provider.StreamChoice{
					{
						Index: geminiResp.Candidates[0].Index,
						Delta: provider.Message{
							Role:    "assistant",
							Content: content,
						},
					},
				},
			}
		}
	}
	return nil
}
