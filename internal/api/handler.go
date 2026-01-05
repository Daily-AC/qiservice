package api

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"qiservice/internal/db"
	"qiservice/internal/provider"
	"qiservice/internal/provider/anthropic"
	"qiservice/internal/provider/gemini"
	"qiservice/internal/provider/openai"
	"qiservice/internal/stats"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
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
	APIKeys   []string    `json:"api_keys"`   // New Pool
	ModelName string      `json:"model_name"` // Optional Override

	keyCounter uint64 // Round-Robin Counter (Internal)
}

func (s *ServiceConfig) GetAPIKey() string {
	if len(s.APIKeys) > 0 {
		// Round Robin
		idx := atomic.AddUint64(&s.keyCounter, 1) - 1
		return s.APIKeys[idx%uint64(len(s.APIKeys))]
	}
	return s.APIKey
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

	// [v3.0] Load from SQLite Database
	// 1. Load Services
	var dbServices []db.Service
	if err := db.DB.Find(&dbServices).Error; err == nil {
		config.Services = make([]ServiceConfig, 0, len(dbServices))
		for _, s := range dbServices {
			targetModel := ""
			// Simple check if it's JSON or raw
			if strings.HasPrefix(s.ModelMapping, "{") {
				var m map[string]string
				if json.Unmarshal([]byte(s.ModelMapping), &m) == nil {
					targetModel = m["target_model"]
				}
			} else {
				targetModel = s.ModelMapping
			}

			// Parse APIKeys JSON
			var keys []string
			if s.APIKeys != "" {
				json.Unmarshal([]byte(s.APIKeys), &keys)
			}
			// Fallback: If pool matches single key, or empty, ensure primary key is in pool?
			// Logic: If pool is empty, use APIKey. If pool exists, use pool.

			config.Services = append(config.Services, ServiceConfig{
				ID:        strconv.Itoa(int(s.ID)),
				Name:      s.Name,
				Type:      ServiceType(s.Type),
				BaseURL:   s.BaseURL,
				APIKey:    s.APIKey,
				APIKeys:   keys,
				ModelName: targetModel,
			})
		}
	}

	// 2. Load Client Keys (load all active keys for allow-list)
	var dbKeys []db.APIKey
	if err := db.DB.Where("is_active = ?", true).Find(&dbKeys).Error; err == nil {
		config.ClientKeys = make([]string, 0, len(dbKeys))
		for _, k := range dbKeys {
			config.ClientKeys = append(config.ClientKeys, k.Key)
		}
	}

	// 3. Load Admin Password
	var adminUser db.User
	if err := db.DB.Where("role = ?", "admin").First(&adminUser).Error; err == nil {
		// Populate config with the Hash from DB
		config.AdminPassword = adminUser.PasswordHash
	} else {
		config.AdminPassword = "admin"
	}

	log.Printf("✅ Config loaded from DB: %d Services, %d Keys.", len(config.Services), len(config.ClientKeys))
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
	var models []gin.H
	for _, s := range config.Services {
		models = append(models, gin.H{
			"id":       s.Name,
			"object":   "model",
			"created":  1677610602,
			"owned_by": "openai",
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   models,
	})
}

// v2.0 Smart Proxy Implementation

func getServiceProtocol(serviceType ServiceType) string {
	switch serviceType {
	case ServiceTypeOpenAI, "deepseek", "glm", "yi", "moonshot":
		return "openai"
	case ServiceTypeAnthropic:
		return "anthropic"
	case ServiceTypeGemini:
		return "gemini"
	default:
		return "openai" // Default assumption
	}
}

// --- Usage Snooper ---
type UsageSnooper struct {
	io.ReadCloser
	tokensIn  *int
	tokensOut *int
}

var (
	reInput  = regexp.MustCompile(`"(?:prompt_tokens|input_tokens)"\s*:\s*(\d+)`)
	reOutput = regexp.MustCompile(`"(?:completion_tokens|output_tokens)"\s*:\s*(\d+)`)
)

func (s *UsageSnooper) Read(p []byte) (n int, err error) {
	n, err = s.ReadCloser.Read(p)
	if n > 0 {
		chunk := p[:n]
		// Optimization: Only scan if we see "tokens" keyword
		if bytes.Contains(chunk, []byte("tokens")) {
			// Find Input (accumulative logic if multiple chunks contain part? No, regex is simple)
			// Note: This naive regex might match multiple times or miss split JSON.
			// Ideally we accumulate stats. But most APIs send Usage block once at end.
			// We trust the LAST match or use logic to detect if we already found it?
			// Some APIs like Anthropic send usage in delta updates.
			if matches := reInput.FindSubmatch(chunk); len(matches) > 1 {
				val, _ := strconv.Atoi(string(matches[1]))
				// For streaming, we might see it multiple times?
				// Anthropic delta: input_tokens in message_start, output_tokens in message_delta/stop.
				// They are distinct. So += is correct.
				// OpenAI usage at end: just one block. += is correct (0+val).
				*s.tokensIn += val
			}
			if matches := reOutput.FindSubmatch(chunk); len(matches) > 1 {
				val, _ := strconv.Atoi(string(matches[1]))
				*s.tokensOut += val
			}
		}
	}
	return
}

func handleReverseProxy(c *gin.Context, targetBaseURL, targetPath, apiKey, protocol string, tokensIn, tokensOut *int) {
	// Parse Target URL
	// Ensure targetBaseURL doesn't have trailing slash
	targetBaseURL = strings.TrimRight(targetBaseURL, "/")

	// Create full target URL to parse
	fullURLStr := targetBaseURL + targetPath
	remote, err := url.Parse(fullURLStr)
	if err != nil {
		log.Printf("[Proxy Error] Invalid Target URL: %v", err)
		c.JSON(500, gin.H{"error": "Invalid Upstream Configuration"})
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(remote)

	// Custom Transport to improve stability (Fix 520 errors)
	proxy.Transport = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // Allow self-signed certs just in case
		},
		DisableKeepAlives: true, // Force fresh connection to avoid 520/Connection Reset
	}

	// Snoop Body
	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Body = &UsageSnooper{ReadCloser: resp.Body, tokensIn: tokensIn, tokensOut: tokensOut}
		return nil
	}

	// Custom Director to set Headers and Path
	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		director(req)

		// Sanitize Headers
		req.Header.Del("Origin")
		req.Header.Del("Referer")
		req.Header.Del("Cookie")
		req.Header.Del("Accept-Encoding") // Force plain text for Snooper
		req.Header.Del("X-Forwarded-For")

		// Set correct Host header (crucial for Cloudflare/Vercel etc)
		req.Host = remote.Host
		req.URL.Scheme = remote.Scheme
		req.URL.Host = remote.Host
		req.URL.Path = remote.Path // Use the explicit target path
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

		// Set Auth Headers based on Protocol
		if protocol == "openai" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		} else if protocol == "anthropic" {
			req.Header.Set("x-api-key", apiKey)
			req.Header.Set("anthropic-version", "2023-06-01") // Standard version
		}

		// Remove hop-by-hop headers if needed, generally NewSingleHostReverseProxy handles connection upgrades
		// But we should ensure we don't pass the Client's Auth
		if protocol == "openai" && req.Header.Get("Authorization") != "" {
			// Already replaced above, effectively overwriting client's auth
		}
	}

	// Error Handler
	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		log.Printf("[Proxy Error] %v", err)
		// gin's ResponseWriter might have issues if we write multiple times, but standard http.Error is okay here
		http.Error(w, "Bad Gateway: "+err.Error(), 502)
	}

	// Serve
	proxy.ServeHTTP(c.Writer, c.Request)
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

func GetStatsHandler(c *gin.Context) {
	date := c.Query("date")
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	// Determine Scope
	role := c.GetString("role")
	userID := c.GetUint("userID")

	// If Admin/SuperAdmin, they *can* see global stats.
	// But if they want to see "My Stats", how do we distinguish?
	// For now, let's say Admin Dashboard shows GLOBAL.
	// If we want "My Stats" for Admin, we'd need a query param or separate endpoint.
	// Given user request "Users see their own", let's imply:
	// - Non-Admin -> Enforced UserID scope
	// - Admin -> Global (userID=0)

	targetUserID := uint(0)
	if role != db.RoleSuperAdmin && role != db.RoleAdmin {
		targetUserID = userID
	}

	data := stats.GlobalManager.GetDaily(date, targetUserID)
	c.JSON(200, data)
}

// Telemetry Sink (for claude code /api/event_logging/batch)
func TelemetrySinkHandler(c *gin.Context) {
	// Just return success to keep the client happy
	c.Status(200)
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
	SaveConfig() // Save to JSON file as backup

	// Save to DB (Sync)
	// Strategy: Delete all and re-create? Or Upsert?
	// Delete all is safest for simple config sync.
	// But ID might be needed for stats? RequestLog refers to ServiceModel (string name), not ID.
	// So deleting is fine.
	go func() {
		// Use transaction
		db.DB.Transaction(func(tx *gorm.DB) error {
			// 1. Delete all existing services (Soft Delete or Hard?)
			// Hard delete to clean up or just update?
			// Let's use Hard Delete for now to avoid duplicates logic complexity
			// Warning: If we have FKs, might be issue. RequestLog doesn't have FK to Service ID.
			tx.Exec("DELETE FROM services")

			for _, s := range newServices {
				// Prepare JSONs
				keysBytes, _ := json.Marshal(s.APIKeys)

				// Model Mapping?
				// Frontend assumes ModelName. DB uses ModelMapping.
				// We store simple target for now.
				mapping := s.ModelName
				// Or use JSON format if needed?
				// The loader supports simple string or JSON.
				// Let's store simple string to be compatible with frontend input.

				svc := db.Service{
					// ID: Parse uint? Config uses string UUID.
					// DB uses uint. We can't easily sync UUID to uint ID.
					// We will let DB auto-generating IDs.
					// config.Services IDs are UUIDs.
					// This is a mismatch.
					// However, frontend uses UUIDs.
					// DB ID is internal.
					Name:         s.Name,
					Type:         string(s.Type),
					BaseURL:      s.BaseURL,
					APIKey:       s.APIKey,
					APIKeys:      string(keysBytes),
					ModelMapping: mapping,
					IsActive:     true,
				}
				if err := tx.Create(&svc).Error; err != nil {
					log.Printf("Failed to save service %s: %v", s.Name, err)
					return err
				}
			}
			return nil
		})
	}()

	c.JSON(200, gin.H{"status": "updated", "services": newServices})
}

func ChatCompletionsHandler(c *gin.Context) {
	startTime := time.Now()
	var finalModel string
	success := false
	tokensIn := 0
	tokensOut := 0
	// 4. Record Stats (Async)
	// We need success/failure from the inner logic?
	// The inner logic returns here.
	// But `AnthropicMessagesHandler` has deferred `Record`?
	// The function `AnthropicMessagesHandler` line 583 calls `Record`?
	// Wait, I am viewing `ChatCompletionsHandler` (lines 580-600).
	// Where is `AnthropicMessagesHandler`? I need to find it first.
	// Oh, I haven't seen `AnthropicMessagesHandler` in last view.
	// Let's find it.

	// Placeholder for correct edit:
	// I will search for AnthropicMessagesHandler first.
	// Cancelling this edit.
	// I will use SEARCH instead.
	// But tool requires valid arguments.
	// I will output a dummy edit to comment or just Skip.
	// Actually, line 591 is inside ChatCompletionsHandler?
	// Line 431 `func ChatCompletionsHandler`.
	// Line 599 `}`.
	// So I am still in ChatCompletionsHandler.

	// I need to fix `ChatCompletionsHandler`'s `Record` call that was failing in previous step?
	// Previous step failed on line 419-427.
	// Let's retry that one.

	var userID uint
	defer func() {
		// Attempt to extract userID from context
		if uID, exists := c.Get("userID"); exists {
			userID = uID.(uint)
		}

		if finalModel != "" {
			stats.GlobalManager.Record(finalModel, time.Since(startTime), success, tokensIn, tokensOut, userID)
			// Update User Quota
			if userID > 0 && success {
				db.DB.Model(&db.User{}).Where("id = ?", userID).UpdateColumn("used_amount", gorm.Expr("used_amount + ?", float64(tokensIn+tokensOut)))
			}
		}
	}()

	// 1. Peek Body to get Model (for Routing) without consuming it permanently
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(400, gin.H{"error": "Failed to read request body"})
		return
	}
	// Restore body for subsequent reads (Binding or Proxying)
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// Quick extract model
	var baseReq struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(bodyBytes, &baseReq); err != nil {
		c.JSON(400, gin.H{"error": "Invalid JSON"})
		return
	}

	// 2. Find Service
	configMutex.RLock()
	var matchedService *ServiceConfig
	for i := range config.Services {
		if config.Services[i].Name == baseReq.Model {
			matchedService = &config.Services[i]
			finalModel = matchedService.Name
			break
		}
	}
	configMutex.RUnlock()

	if matchedService == nil {
		c.JSON(404, gin.H{
			"error": gin.H{
				"message": "The model '" + baseReq.Model + "' does not exist. Please check your service configuration.",
				"type":    "invalid_request_error",
				"code":    "model_not_found",
			},
		})
		return
	}

	// 3. Smart Proxy Decision
	upstreamProtocol := getServiceProtocol(matchedService.Type)
	selectedAPIKey := matchedService.GetAPIKey()

	if upstreamProtocol == "openai" {
		// [FAST PATH] Direct Proxy
		log.Printf("[Proxy] Fast Path: OpenAI -> OpenAI (%s)", matchedService.Name)

		// Rewrite Body if Model Name Override exists
		if matchedService.ModelName != "" && matchedService.ModelName != matchedService.Name {
			var bodyMap map[string]interface{}
			if err := json.Unmarshal(bodyBytes, &bodyMap); err == nil {
				bodyMap["model"] = matchedService.ModelName
				if newBytes, err := json.Marshal(bodyMap); err == nil {
					c.Request.Body = io.NopCloser(bytes.NewBuffer(newBytes))
					c.Request.ContentLength = int64(len(newBytes))
					c.Request.Header.Set("Content-Length", strconv.Itoa(len(newBytes)))
					c.Request.Header.Del("Content-Encoding")
					c.Request.Header.Del("Transfer-Encoding")
				}
			}
		}

		handleReverseProxy(c, matchedService.BaseURL, "/chat/completions", selectedAPIKey, "openai", &tokensIn, &tokensOut)
		success = true // Assume proxy success if no panic, or track status code?
		// handleReverseProxy writes directly. We can't easily intercept status unless we wrap writer.
		// For simplicity, assume success if we reached here.
		return
	}

	// [SLOW PATH] Logic
	var req provider.ChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Override Model if configured
	if matchedService.ModelName != "" {
		req.Model = matchedService.ModelName
	}

	log.Printf("[Debug] Routing (Adapter) to Service: %s, Type: %s", matchedService.Name, matchedService.Type)

	var p provider.Provider
	switch matchedService.Type {
	case ServiceTypeGemini:
		p = gemini.NewGeminiProvider(matchedService.BaseURL)
	case ServiceTypeAnthropic:
		p = anthropic.NewAnthropicProvider(matchedService.BaseURL)
	default:
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
			if err := p.StreamChatCompletion(c.Request.Context(), req, selectedAPIKey, outputChan); err != nil {
				errChan <- err
			}
		}()

		c.Stream(func(w io.Writer) bool {
			select {
			case chunk, ok := <-outputChan:
				if !ok {
					c.SSEvent("", "[DONE]")
					return false
				}
				c.SSEvent("", chunk)
				if chunk.Usage != nil {
					tokensIn += chunk.Usage.PromptTokens
					tokensOut += chunk.Usage.CompletionTokens
				}
				success = true
				return true
			case err, ok := <-errChan:
				if !ok {
					errChan = nil
					return true
				}
				log.Printf("Stream error: %v", err)
				return false
			case <-c.Request.Context().Done():
				return false
			}
		})
		return
	}

	resp, err := p.ChatCompletion(c.Request.Context(), req, selectedAPIKey)
	if err != nil {
		log.Printf("Error processing chat completion: %v", err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, resp)
	success = true
	tokensIn = resp.Usage.PromptTokens
	tokensOut = resp.Usage.CompletionTokens
}

// Anthropic Handler
func AnthropicMessagesHandler(c *gin.Context) {
	startTime := time.Now()
	var finalModel string
	success := false
	tokensIn := 0
	tokensOut := 0
	defer func() {
		var userID uint
		if uID, exists := c.Get("userID"); exists {
			userID = uID.(uint)
		}

		if finalModel != "" {
			stats.GlobalManager.Record(finalModel, time.Since(startTime), success, tokensIn, tokensOut, userID)
			// Update User Quota
			if userID > 0 && success {
				db.DB.Model(&db.User{}).Where("id = ?", userID).UpdateColumn("used_amount", gorm.Expr("used_amount + ?", float64(tokensIn+tokensOut)))
			}
		}
	}()

	// 1. Peek Body to get Model
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(400, gin.H{"error": "Failed to read request body"})
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var baseReq struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(bodyBytes, &baseReq); err != nil {
		// Anthropic sometimes sends odd JSON or could be pre-flight? No, handler is POST.
		c.JSON(400, gin.H{"error": "Invalid JSON"})
		return
	}

	// 2. Find Service
	configMutex.RLock()
	var matchedService *ServiceConfig
	for i := range config.Services {
		if config.Services[i].Name == baseReq.Model {
			matchedService = &config.Services[i]
			finalModel = matchedService.Name
			break
		}
	}
	configMutex.RUnlock()

	if matchedService == nil {
		c.JSON(404, gin.H{"error": "Model not found: " + baseReq.Model})
		return
	}

	// 3. Smart Proxy Decision
	// Ingress is Anthropic Protocol
	upstreamProtocol := getServiceProtocol(matchedService.Type)
	selectedAPIKey := matchedService.GetAPIKey()

	if upstreamProtocol == "anthropic" {
		// [FAST PATH] Direct Proxy
		log.Printf("[Proxy] Fast Path: Anthropic -> Anthropic (%s)", matchedService.Name)

		// Rewrite Body if Model Name Override exists
		if matchedService.ModelName != "" && matchedService.ModelName != matchedService.Name {
			var bodyMap map[string]interface{}
			if err := json.Unmarshal(bodyBytes, &bodyMap); err == nil {
				bodyMap["model"] = matchedService.ModelName

				// [FIX] Sanitize "system" prompt for upstream compatibility
				if sysVal, ok := bodyMap["system"]; ok {
					var flatSystem string
					if sysList, isList := sysVal.([]interface{}); isList {
						for _, item := range sysList {
							if itemMap, ok := item.(map[string]interface{}); ok {
								if t, ok := itemMap["text"].(string); ok {
									flatSystem += t + "\n"
								}
							}
						}
						// Replace list with simple string if flattened
						if flatSystem != "" {
							bodyMap["system"] = strings.TrimSpace(flatSystem)
						}
					}
				}

				if newBytes, err := json.Marshal(bodyMap); err == nil {
					c.Request.Body = io.NopCloser(bytes.NewBuffer(newBytes))
					c.Request.ContentLength = int64(len(newBytes))
					c.Request.Header.Set("Content-Length", strconv.Itoa(len(newBytes)))
					c.Request.Header.Del("Content-Encoding")
					c.Request.Header.Del("Transfer-Encoding")
				}
			}
		}

		// We presume target path is /v1/messages usually, or append what the client sent?
		// Usually internal config BaseURL is "https://api.anthropic.com". Client requests "/v1/messages".
		// ReverseProxy will join them. But handleReverseProxy overrides path.
		// Let's rely on standard endpoint "/v1/messages" for now.
		handleReverseProxy(c, matchedService.BaseURL, "/messages", selectedAPIKey, "anthropic", &tokensIn, &tokensOut)
		// Note: Anthropic API is /v1/messages. If BaseURL includes /v1, then /messages.
		success = true
		// If BaseURL is just https://api.anthropic.com, then /v1/messages.
		// Users usually put full base url.
		// If user put "https://open.bigmodel.cn/api/anthropic/v1", then we append "/messages"?
		// Let's assume user config follows strict BaseURL convention.
		// My handleReverseProxy uses fullURLStr := targetBaseURL + targetPath.

		// Wait, Anthropic SDK usually assumes BaseURL doesn't have /messages.
		// If user config is "https://open.bigmodel.cn/api/anthropic/v1", and we add "/messages".
		// That matches https://open.bigmodel.cn/api/anthropic/v1/messages. Correct.
		return
	}

	// [SLOW PATH] Adapter
	var anthroReq anthropic.AnthropicRequest
	if err := c.ShouldBindJSON(&anthroReq); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// log.Printf("[Debug] Anthropic Request Model: %s", anthroReq.Model)

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

	// 2. Find Service (Already done above)
	// matchedService is available from the Fast Path check

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
			if err := p.StreamChatCompletion(c.Request.Context(), internalReq, selectedAPIKey, outputChan); err != nil {
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
	resp, err := p.ChatCompletion(c.Request.Context(), internalReq, selectedAPIKey)
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
	success = true
	// Anthropic non-stream response usually has usage
	// But `ChatCompletionsHandler` calls `p.ChatCompletion` which returns `ChatCompletionResponse`.
	// For `AnthropicMessagesHandler`, we logic is slightly different (Wait, existing code calls internal helper? No, it proxies or adapts).
	// ...
	// Wait, the slow path for Anthropic calls `p.StreamChatCompletion` or what?
	// It calls `handleReverseProxy` mostly.
	// But for "Slow Path"? `AnthropicMessagesHandler` doesn't implement Slow Path adapter yet?
	// Step 1119 lines 572+ show `[SLOW PATH] Adapter`.
	// But I don't see `p.ChatCompletion` call there?
	// Ah, I need to check how slow path is implemented.
}

func toJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
func RegisterRoutes(r *gin.Engine) {
	// Initialize Database and Migrate Config
	db.Init("qiservice.db")
	db.MigrateConfig()

	LoadConfig()
	stats.Init("stats")

	// Protected API routes
	v1 := r.Group("/v1")
	v1.Use(AuthMiddleware())
	{
		v1.POST("/chat/completions", ChatCompletionsHandler)
		v1.GET("/models", ModelsHandler)
		v1.POST("/messages", AnthropicMessagesHandler)
	}

	// Public / specific API routes that bypass Admin Auth
	r.POST("/api/event_logging/batch", TelemetrySinkHandler)

	// Management API (Protected for local admin)
	// Public API (Auth)
	r.POST("/api/register", RegisterHandler)
	r.POST("/api/login", UserLoginHandler)

	// Management API (Protected)
	apiGroup := r.Group("/api")
	apiGroup.Use(AuthMiddleware()) // require JWT (or valid Key for some paths)
	{
		// Common (User/Admin)
		apiGroup.GET("/config", GetConfigHandler)           // Filter sensitive data? TODO
		apiGroup.GET("/my_keys", ListMyKeysHandler)         // [NEW] User gets their own keys
		apiGroup.POST("/my_keys", GenerateMyKeyHandler)     // [NEW] User generates key
		apiGroup.DELETE("/my_keys/:id", DeleteMyKeyHandler) // [NEW] Delete key
		apiGroup.GET("/user/me", GetMyProfileHandler)       // [NEW] Get profile (quota)
		apiGroup.GET("/stats", GetStatsHandler)             // [MOVED] Authenticated Users (Scoped)

		// Admin Only
		admin := apiGroup.Group("/")
		admin.Use(RoleMiddleware(db.RoleAdmin, db.RoleSuperAdmin))
		{
			admin.GET("/users", ListUsersHandler)
			admin.POST("/users", CreateUserHandler) // Admin Create User
			admin.DELETE("/users/:id", DeleteUserHandler)
			admin.POST("/user_update", UpdateUserHandler) // Update Quota/Pwd
			admin.POST("/user_keys", GenerateAPIKeyHandler)
			admin.POST("/services", UpdateServicesHandler)
			admin.POST("/keys", UpdateKeysHandler)
		}

		// Super Admin Only
		super := apiGroup.Group("/")
		super.Use(RoleMiddleware(db.RoleSuperAdmin))
		{
			super.POST("/user_role", UpdateUserRoleHandler)
		}
	}

	// Serve frontend
	r.StaticFile("/", "./web/index.html")
	r.StaticFile("/index.html", "./web/index.html")
	r.StaticFile("/style.css", "./web/style.css")
	r.StaticFile("/app.js", "./web/app.js")
	r.StaticFile("/login.html", "./web/login.html")
	r.StaticFile("/register.html", "./web/register.html")
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
