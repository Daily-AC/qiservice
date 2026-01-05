package stats

import (
	"log"
	"time"

	"qiservice/internal/db"
)

// Legacy compatibility structs
type ModelStats struct {
	Requests int `json:"request_count"` // Updated json tag to match frontend expectation or fix frontend?
	// Frontend app.js expects: data.summary[m].request_count
	// Let's check app.js: const reqs = models.map(m => data.summary[m].request_count);
	// Wait, original JSON used "requests"?
	// manager.go:21 Requests  int `json:"requests"`
	// app.js:175 const reqs = models.map(m => data.summary[m].request_count);
	// Mismatch?
	// Let's re-read app.js snippet earlier.
	// app.js:175: const reqs = models.map(m => data.summary[m].request_count);
	// But manager.go had `Requests int `json:"requests"``
	// If it was mismatch, charts wouldn't work before?
	// The user said "Blue box no data". So charts WERE broken or empty.
	// If I fix it now, I should use "request_count" to match app.js or update app.js.
	// Let's update struct to match app.js expectation: "request_count"
	// Also "input_tokens", "output_tokens".

	// Re-checking manager.go original:
	// TokensIn  int `json:"tokens_in"`
	// TokensOut int `json:"tokens_out"`

	// app.js:
	// data.summary[m].input_tokens
	// data.summary[m].output_tokens

	// So the original manager.go keys were "requests", "tokens_in", "tokens_out".
	// app.js uses "request_count", "input_tokens", "output_tokens".
	// That explains why charts were empty/undefined!

	TokensIn  int `json:"input_tokens"`
	TokensOut int `json:"output_tokens"`
}

type DailyStats struct {
	Date     string                `json:"date"`
	Summary  map[string]ModelStats `json:"summary"` // Model -> Stats
	TotalReq int64                 `json:"total_requests"`
}

type Manager struct{}

var GlobalManager *Manager

func Init(dataDir string) {
	// DB is already init
	GlobalManager = &Manager{}
}

func (m *Manager) Record(model string, duration time.Duration, success bool, tokensIn, tokensOut int) {
	// Async insert to not block
	go func() {
		status := 200
		if !success {
			status = 500
		}
		logEntry := db.RequestLog{
			ServiceModel:     model,
			DurationMs:       duration.Milliseconds(),
			Status:           status,
			PromptTokens:     tokensIn,
			CompletionTokens: tokensOut,
			CreatedAt:        time.Now(),
		}
		// We don't have UserID passed here easily yet.
		// Context has it, but Record is called from handler's defer.
		// For now, we log anonymous or 0. Enhancing later if needed.

		if err := db.DB.Create(&logEntry).Error; err != nil {
			log.Printf("[Stats] Failed to record: %v", err)
		}
	}()
}

func (m *Manager) GetDaily(date string) *DailyStats {
	// Parse Date Range (Use Local Time to match Record)
	start, _ := time.ParseInLocation("2006-01-02", date, time.Local)
	end := start.Add(24 * time.Hour)

	// Optimize: Use aggregation query instead of fetching all rows
	// SELECT service_model, COUNT(*), SUM(prompt_tokens), SUM(completion_tokens) FROM request_logs WHERE ... GROUP BY service_model

	type Result struct {
		ServiceModel  string
		Count         int
		SumPrompt     int
		SumCompletion int
	}

	var results []Result
	db.DB.Model(&db.RequestLog{}).
		Select("service_model, count(*) as count, sum(prompt_tokens) as sum_prompt, sum(completion_tokens) as sum_completion").
		Where("created_at >= ? AND created_at < ?", start, end).
		Group("service_model").
		Scan(&results)

	stats := &DailyStats{
		Date:    date,
		Summary: make(map[string]ModelStats),
	}

	var total int64 = 0
	for _, r := range results {
		stats.Summary[r.ServiceModel] = ModelStats{
			Requests:  r.Count,
			TokensIn:  r.SumPrompt,
			TokensOut: r.SumCompletion,
		}
		total += int64(r.Count)
	}
	stats.TotalReq = total

	return stats
}
