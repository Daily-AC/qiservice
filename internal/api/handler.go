package api

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"qiservice/internal/provider"
	"qiservice/internal/provider/anthropic"
	"qiservice/internal/provider/gemini"
	"qiservice/internal/provider/openai"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ServiceType string

const (
	ServiceTypeOpenAI    ServiceType = "openai"
	ServiceTypeGemini    ServiceType = "gemini"
	ServiceTypeAnthropic ServiceType = "anthropic"
)

type ServiceConfig struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Type      ServiceType `json:"type"`
	BaseURL   string      `json:"base_url"`
	APIKey    string      `json:"api_key"`
	ModelName string      `json:"model_name"` // Optional Override
}

type Config struct {
	Services        []ServiceConfig `json:"services"`
	ActiveServiceId string          `json:"active_service_id"`
	ClientKeys      []string        `json:"client_keys"`
	AdminPassword   string          `json:"admin_password"`
}

var (
	config      Config
	configMutex sync.RWMutex
	configFile  = "config.json"
)

func LoadConfig() {
	configMutex.Lock()
	defer configMutex.Unlock()

	data, err := os.ReadFile(configFile)
	if err == nil {
		json.Unmarshal(data, &config)
	}
	// Init if empty
	if config.Services == nil {
		config.Services = []ServiceConfig{}
	}
	if config.ClientKeys == nil {
		config.ClientKeys = []string{}
	}
	if config.AdminPassword == "" {
		// Generate random password if not set
		config.AdminPassword = uuid.New().String()
		log.Printf("⚠️  ADMIN PASSWORD NOT SET. GENERATED: %s", config.AdminPassword)
		saveConfigInternal() // Save immediately so it persists (without locking)
	} else {
		log.Printf("🔒 Admin Password Loaded.")
	}
}

func SaveConfig() {
	configMutex.RLock()
	defer configMutex.RUnlock()
	saveConfigInternal()
}

func saveConfigInternal() {
	data, _ := json.MarshalIndent(config, "", "  ")
	os.WriteFile(configFile, data, 0644)
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := ""

		// Check x-api-key first (Anthropic style)
		apiKey := c.GetHeader("x-api-key")
		if apiKey != "" {
			token = apiKey
		} else {
			// Check Authorization header (OpenAI style)
			authHeader := c.GetHeader("Authorization")
			if authHeader == "" {
				c.AbortWithStatusJSON(401, gin.H{"error": "Authorization header required"})
				return
			}
			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || parts[0] != "Bearer" {
				c.AbortWithStatusJSON(401, gin.H{"error": "Invalid authorization header format"})
				return
			}
			token = parts[1]
		}

		configMutex.RLock()
		defer configMutex.RUnlock()

		valid := false
		for _, key := range config.ClientKeys {
			if key == token {
				valid = true
				break
			}
		}

		if !valid {
			c.AbortWithStatusJSON(401, gin.H{"error": "Invalid API Key"})
			return
		}

		c.Next()
	}
}

// Admin Authentication Middleware
func AdminAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Public endpoints under /api if any? Currently none except Login
		if c.Request.URL.Path == "/api/login" {
			c.Next()
			return
		}

		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(401, gin.H{"error": "Authorization header required"})
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.AbortWithStatusJSON(401, gin.H{"error": "Invalid authorization header format"})
			return
		}

		token := parts[1]
		configMutex.RLock()
		valid := (token == config.AdminPassword)
		configMutex.RUnlock()

		if !valid {
			c.AbortWithStatusJSON(401, gin.H{"error": "Invalid Admin Password"})
			return
		}

		c.Next()
	}
}

// Login Handler
func LoginHandler(c *gin.Context) {
	var req struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	configMutex.RLock()
	valid := (req.Password == config.AdminPassword)
	configMutex.RUnlock()

	if valid {
		c.JSON(200, gin.H{"status": "ok", "token": req.Password})
	} else {
		c.JSON(401, gin.H{"error": "Invalid password"})
	}
}

// Models Handler
func ModelsHandler(c *gin.Context) {
	configMutex.RLock()
	defer configMutex.RUnlock()

	type ModelData struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}

	models := []ModelData{}
	now := time.Now().Unix()

	for _, s := range config.Services {
		// Ensure unique model IDs if user configured duplicates, though we assume unique names
		models = append(models, ModelData{
			ID:      s.Name, // The Service Name IS the Model ID
			Object:  "model",
			Created: now,
			OwnedBy: "llm-station",
		})
	}

	c.JSON(200, gin.H{
		"object": "list",
		"data":   models,
	})
}

// Client Keys Handlers
func UpdateKeysHandler(c *gin.Context) {
	var newKeys []string
	if err := c.ShouldBindJSON(&newKeys); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	configMutex.Lock()
	config.ClientKeys = newKeys
	configMutex.Unlock()
	SaveConfig()
	c.JSON(200, gin.H{"status": "updated", "keys": newKeys})
}
func GetConfigHandler(c *gin.Context) {
	configMutex.RLock()
	defer configMutex.RUnlock()
	c.JSON(200, config)
}

func UpdateServicesHandler(c *gin.Context) {
	var newServices []ServiceConfig
	if err := c.ShouldBindJSON(&newServices); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Ensure IDs
	for i := range newServices {
		if newServices[i].ID == "" {
			newServices[i].ID = uuid.New().String()
		}
	}

	configMutex.Lock()
	config.Services = newServices
	configMutex.Unlock()
	SaveConfig()
	c.JSON(200, gin.H{"status": "updated", "services": newServices})
}

func ChatCompletionsHandler(c *gin.Context) {
	var req provider.ChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	configMutex.RLock()
	var matchedService *ServiceConfig

	// Routing Logic: Find service by Name == req.Model
	for _, s := range config.Services {
		if s.Name == req.Model {
			val := s
			matchedService = &val
			break
		}
	}
	configMutex.RUnlock()

	if matchedService == nil {
		c.JSON(404, gin.H{
			"error": gin.H{
				"message": "The model '" + req.Model + "' does not exist. Please check your service configuration.",
				"type":    "invalid_request_error",
				"code":    "model_not_found",
			},
		})
		return
	}

	// Override Model if configured
	if matchedService.ModelName != "" {
		req.Model = matchedService.ModelName
	}

	log.Printf("[Debug] Routing (OpenAI Endpoint) to Service: %s, Type: %s", matchedService.Name, matchedService.Type)

	var p provider.Provider

	switch matchedService.Type {
	case ServiceTypeGemini:
		p = gemini.NewGeminiProvider(matchedService.BaseURL)
	case ServiceTypeAnthropic:
		log.Printf("[Debug] Using Anthropic Provider (via OpenAI Endpoint)")
		p = anthropic.NewAnthropicProvider(matchedService.BaseURL)
	default: // OpenAI
		log.Printf("[Debug] Using OpenAI Provider (via OpenAI Endpoint)")
		p = openai.NewOpenAIProvider(matchedService.BaseURL)
	}

	// Check for Streaming
	if req.Stream {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("Transfer-Encoding", "chunked")

		outputChan := make(chan provider.StreamResponse)
		errChan := make(chan error)

		go func() {
			defer close(outputChan)
			defer close(errChan)
			if err := p.StreamChatCompletion(c.Request.Context(), req, matchedService.APIKey, outputChan); err != nil {
				errChan <- err
			}
		}()

		c.Stream(func(w io.Writer) bool {
			select {
			case chunk, ok := <-outputChan:
				if !ok {
					// Stream finished
					c.SSEvent("", "[DONE]")
					return false
				}
				c.SSEvent("", chunk)
				return true
			case err, ok := <-errChan:
				if !ok {
					errChan = nil
					return true // Continue stream
				}
				log.Printf("Stream error: %v", err)
				return false
			case <-c.Request.Context().Done():
				return false
			}
		})
		return
	}

	resp, err := p.ChatCompletion(c.Request.Context(), req, matchedService.APIKey)
	if err != nil {
		// Log specific error
		log.Printf("Error processing chat completion: %v", err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, resp)
}

// Anthropic Handler
func AnthropicMessagesHandler(c *gin.Context) {
	var anthroReq anthropic.AnthropicRequest
	if err := c.ShouldBindJSON(&anthroReq); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Debug Request
	// reqBytes, _ := json.MarshalIndent(anthroReq, "", "  ")
	// log.Printf("[Debug] Anthropic Request: %s", string(reqBytes))

	// 1. Convert Anthropic Request -> Internal Request
	messages := []provider.Message{}

	systemContent := anthropic.ExtractText(anthroReq.System)
	if systemContent != "" {
		messages = append(messages, provider.Message{Role: "system", Content: systemContent})
	}

	for _, m := range anthroReq.Messages {
		// Handle Content List (Anthropic supports mixed content: text, tool_use, tool_result)
		var contentList []map[string]interface{}
		if list, ok := m.Content.([]interface{}); ok {
			for _, item := range list {
				if v, ok := item.(map[string]interface{}); ok {
					contentList = append(contentList, v)
				}
			}
		} else if s, ok := m.Content.(string); ok {
			// Simple string content
			messages = append(messages, provider.Message{Role: m.Role, Content: s})
			continue
		}

		if len(contentList) == 0 {
			// Fallback (empty or unexpected format)
			messages = append(messages, provider.Message{Role: m.Role, Content: ""})
			continue
		}

		// Process blocks
		var textParts []string
		var toolCalls []provider.ToolCall

		// Pre-scan to group text or gather tool calls
		for _, block := range contentList {
			bType, _ := block["type"].(string)

			if bType == "text" {
				if t, ok := block["text"].(string); ok {
					textParts = append(textParts, t)
				}
			} else if bType == "tool_use" {
				// Parse Tool Call (Assistant Side)
				id, _ := block["id"].(string)
				name, _ := block["name"].(string)
				input := block["input"] // JSON object

				inputBytes, _ := json.Marshal(input)

				toolCalls = append(toolCalls, provider.ToolCall{
					ID:   id,
					Type: "function",
					Function: provider.FunctionCall{
						Name:      name,
						Arguments: string(inputBytes),
					},
				})
			} else if bType == "tool_result" {
				// Parse Tool Result (User Side -> Convert to Tool Role Message)
				// Flush any accumulated text as a User message first
				if len(textParts) > 0 {
					messages = append(messages, provider.Message{
						Role:    "user",
						Content: strings.Join(textParts, "\n"),
					})
					textParts = []string{} // Clear
				}

				toolUseID, _ := block["tool_use_id"].(string)
				// Result content can be string or list of blocks (text/image)
				// For now, simplify to string extraction or raw content
				resultContent := ""
				if rc, ok := block["content"].(string); ok {
					resultContent = rc
				} else if rList, ok := block["content"].([]interface{}); ok {
					// extract text from result blocks
					for _, rItem := range rList {
						if rMap, ok := rItem.(map[string]interface{}); ok {
							if rt, ok := rMap["type"].(string); ok && rt == "text" {
								if rTxt, ok := rMap["text"].(string); ok {
									resultContent += rTxt
								}
							}
						}
					}
				}

				messages = append(messages, provider.Message{
					Role:       "tool",
					ToolCallID: toolUseID,
					Content:    resultContent,
				})
			}
		}

		// Final Flush for this message
		// If it's assistant with tool calls
		if m.Role == "assistant" && len(toolCalls) > 0 {
			msg := provider.Message{
				Role:      "assistant",
				ToolCalls: toolCalls,
			}
			if len(textParts) > 0 {
				msg.Content = strings.Join(textParts, "\n")
			}
			messages = append(messages, msg)
		} else if m.Role == "user" && len(textParts) > 0 {
			// Remaining extracted text
			messages = append(messages, provider.Message{
				Role:    "user",
				Content: strings.Join(textParts, "\n"),
			})
		} else if m.Role == "assistant" && len(textParts) > 0 && len(toolCalls) == 0 {
			// Assistant text only
			messages = append(messages, provider.Message{
				Role:    "assistant",
				Content: strings.Join(textParts, "\n"),
			})
		}
	}

	internalReq := provider.ChatCompletionRequest{
		Model:    anthroReq.Model,
		Messages: messages,
		Stream:   anthroReq.Stream,
	}

	// 1.5 Map Tools
	if len(anthroReq.Tools) > 0 {
		log.Printf("[DEBUG] Request contains %d tools", len(anthroReq.Tools)) // Debug log
		internalReq.Tools = []provider.Tool{}
		for _, t := range anthroReq.Tools {
			// log.Printf("[DEBUG] Tool: %s", t.Name)
			internalReq.Tools = append(internalReq.Tools, provider.Tool{
				Type: "function",
				Function: provider.ToolFunction{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.InputSchema,
				},
			})
		}
	}

	// 2. Find Service
	configMutex.RLock()
	var matchedService *ServiceConfig
	for _, s := range config.Services {
		if s.Name == internalReq.Model {
			val := s
			matchedService = &val
			break
		}
	}
	configMutex.RUnlock()

	if matchedService == nil {
		c.JSON(404, gin.H{"error": gin.H{"type": "not_found_error", "message": "Model not found"}})
		return
	}

	log.Printf("[Debug] Routing to Service: %s, Type: %s, URL: %s", matchedService.Name, matchedService.Type, matchedService.BaseURL)

	if matchedService.ModelName != "" {
		internalReq.Model = matchedService.ModelName
	}

	var p provider.Provider
	switch matchedService.Type {
	case ServiceTypeGemini:
		p = gemini.NewGeminiProvider(matchedService.BaseURL)
	case ServiceTypeAnthropic:
		log.Printf("[Debug] Using Anthropic Provider")
		p = anthropic.NewAnthropicProvider(matchedService.BaseURL)
	default:
		log.Printf("[Debug] Using OpenAI Provider (Default)")
		p = openai.NewOpenAIProvider(matchedService.BaseURL)
	}

	// 3. Handle Streaming
	if internalReq.Stream {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("Transfer-Encoding", "chunked")

		outputChan := make(chan provider.StreamResponse)
		errChan := make(chan error)

		go func() {
			defer close(outputChan)
			defer close(errChan)
			if err := p.StreamChatCompletion(c.Request.Context(), internalReq, matchedService.APIKey, outputChan); err != nil {
				errChan <- err
			}
		}()

		// Send 'message_start' event
		msgID := "msg_" + uuid.New().String()
		// We format data manually for Anthropic SSE to ensure exact compliance if gin.SSEvent behaves weirdly with event names
		// But here we use standard gin SSEvent (Event, Data)

		c.Writer.WriteString("event: message_start\n")
		c.Writer.WriteString("data: " + toJSON(gin.H{
			"type": "message_start",
			"message": gin.H{
				"id": msgID, "type": "message", "role": "assistant", "model": anthroReq.Model,
				"usage":   gin.H{"input_tokens": 0, "output_tokens": 0},
				"content": []interface{}{},
			},
		}) + "\n\n")
		c.Writer.Flush()

		// Keep track of current block index
		blockIndex := 0
		inToolUse := false

		// Initial text block
		c.Writer.WriteString("event: content_block_start\n")
		c.Writer.WriteString("data: " + toJSON(gin.H{"type": "content_block_start", "index": blockIndex, "content_block": gin.H{"type": "text", "text": ""}}) + "\n\n")
		c.Writer.Flush()

		c.Stream(func(w io.Writer) bool {
			select {
			case chunk, ok := <-outputChan:
				if !ok {
					c.Writer.WriteString("event: content_block_stop\n")
					c.Writer.WriteString("data: " + toJSON(gin.H{"type": "content_block_stop", "index": blockIndex}) + "\n\n")

					c.Writer.WriteString("event: message_delta\n")
					c.Writer.WriteString("data: " + toJSON(gin.H{"type": "message_delta", "delta": gin.H{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": gin.H{"output_tokens": 0}}) + "\n\n")

					c.Writer.WriteString("event: message_stop\n")
					c.Writer.WriteString("data: " + toJSON(gin.H{"type": "message_stop"}) + "\n\n")
					return false
				}

				if len(chunk.Choices) > 0 {
					delta := chunk.Choices[0].Delta

					// Case A: Text Content
					if delta.Content != "" {
						if inToolUse {
							// Close previous tool block if we switch back to text (rare in streaming but possible)
							c.Writer.WriteString("event: content_block_stop\n")
							c.Writer.WriteString("data: " + toJSON(gin.H{"type": "content_block_stop", "index": blockIndex}) + "\n\n")
							blockIndex++
							inToolUse = false

							// Start new text block
							c.Writer.WriteString("event: content_block_start\n")
							c.Writer.WriteString("data: " + toJSON(gin.H{"type": "content_block_start", "index": blockIndex, "content_block": gin.H{"type": "text", "text": ""}}) + "\n\n")
							c.Writer.Flush()
						}

						c.Writer.WriteString("event: content_block_delta\n")
						c.Writer.WriteString("data: " + toJSON(gin.H{
							"type":  "content_block_delta",
							"index": blockIndex,
							"delta": gin.H{"type": "text_delta", "text": delta.Content},
						}) + "\n\n")
						c.Writer.Flush()
					}

					// Case B: Tool Calls
					if len(delta.ToolCalls) > 0 {
						log.Printf("[DEBUG] Rx ToolCall: %+v", delta.ToolCalls[0])
						if !inToolUse || delta.ToolCalls[0].ID != "" {
							if !inToolUse && blockIndex == 0 {
								// Close the initial empty text block if we go straight to tools
								// (Optional optimization: some clients might expect at least one text block)
								c.Writer.WriteString("event: content_block_stop\n")
								c.Writer.WriteString("data: " + toJSON(gin.H{"type": "content_block_stop", "index": blockIndex}) + "\n\n")
								blockIndex++
							} else if inToolUse && delta.ToolCalls[0].ID != "" {
								// Close previous tool block
								c.Writer.WriteString("event: content_block_stop\n")
								c.Writer.WriteString("data: " + toJSON(gin.H{"type": "content_block_stop", "index": blockIndex}) + "\n\n")
								blockIndex++
							}

							inToolUse = true
							// Start Tool Block
							toolCall := delta.ToolCalls[0]
							c.Writer.WriteString("event: content_block_start\n")
							c.Writer.WriteString("data: " + toJSON(gin.H{
								"type":  "content_block_start",
								"index": blockIndex,
								"content_block": gin.H{
									"type":  "tool_use",
									"id":    toolCall.ID,
									"name":  toolCall.Function.Name,
									"input": gin.H{}, // Start empty, fill via delta
								},
							}) + "\n\n")
							c.Writer.Flush()
						}

						if delta.ToolCalls[0].Function.Arguments != "" {
							c.Writer.WriteString("event: content_block_delta\n")
							c.Writer.WriteString("data: " + toJSON(gin.H{
								"type":  "content_block_delta",
								"index": blockIndex,
								"delta": gin.H{"type": "input_json_delta", "partial_json": delta.ToolCalls[0].Function.Arguments},
							}) + "\n\n")
							c.Writer.Flush()
						}
					}
				}
				return true
			case err, ok := <-errChan:
				if !ok {
					errChan = nil
					return true // Continue stream
				}
				log.Printf("[ERROR] Stream Error: %v", err)
				return false
			case <-c.Request.Context().Done():
				return false
			}
		})
		return
	}

	// 4. Handle Non-Streaming
	resp, err := p.ChatCompletion(c.Request.Context(), internalReq, matchedService.APIKey)
	if err != nil {
		c.JSON(500, gin.H{"error": gin.H{"type": "api_error", "message": err.Error()}})
		return
	}

	// Convert Response -> Anthropic
	content := ""
	if len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
	}
	anthroResp := anthropic.AnthropicResponse{
		ID:      resp.ID,
		Type:    "message",
		Role:    "assistant",
		Content: []anthropic.AnthropicContent{{Type: "text", Text: content}},
	}

	c.JSON(200, anthroResp)
}

func toJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
func RegisterRoutes(r *gin.Engine) {
	LoadConfig()

	// Protected API routes
	v1 := r.Group("/v1")
	v1.Use(AuthMiddleware())
	{
		v1.POST("/chat/completions", ChatCompletionsHandler)
		v1.GET("/models", ModelsHandler)
		v1.POST("/messages", AnthropicMessagesHandler)
	}

	// Management API (Protected for local admin)
	apiGroup := r.Group("/api")
	apiGroup.Use(AdminAuthMiddleware()) // Protect all /api endpoints
	{
		apiGroup.GET("/config", GetConfigHandler)
		apiGroup.POST("/services", UpdateServicesHandler) // Update full list
		apiGroup.POST("/keys", UpdateKeysHandler)         // Update key list
		apiGroup.POST("/login", LoginHandler)             // Actually handled by middleware exception, but good to be explicit or move out
	}

	// Serve frontend
	r.StaticFile("/", "./web/index.html")
	r.StaticFile("/index.html", "./web/index.html")
	r.StaticFile("/style.css", "./web/style.css")
	r.StaticFile("/app.js", "./web/app.js")
	// Also keep /web for direct access if needed
	r.Static("/web", "./web")

	r.NoRoute(func(c *gin.Context) {
		// Verify if it is an API request to return JSON 404
		if len(c.Request.URL.Path) >= 4 && c.Request.URL.Path[:4] == "/api" {
			c.JSON(404, gin.H{"error": "not found"})
			return
		}
		c.File("./web/index.html")
	})
}
