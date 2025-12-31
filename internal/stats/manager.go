package stats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type RequestRecord struct {
	Time      time.Time `json:"time"`
	Model     string    `json:"model"`
	Duration  float64   `json:"duration_ms"`
	Success   bool      `json:"success"`
	TokensIn  int       `json:"tokens_in,omitempty"`
	TokensOut int       `json:"tokens_out,omitempty"`
}

type ModelStats struct {
	Requests  int `json:"requests"`
	TokensIn  int `json:"tokens_in"`
	TokensOut int `json:"tokens_out"`
}

type DailyStats struct {
	Date     string                `json:"date"`
	Records  []RequestRecord       `json:"records"`
	Summary  map[string]ModelStats `json:"summary"` // Model -> Stats
	TotalReq int                   `json:"total_requests"`
}

type Manager struct {
	mu      sync.Mutex
	dataDir string
}

var GlobalManager *Manager

func Init(dataDir string) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		panic(err)
	}
	GlobalManager = &Manager{
		dataDir: dataDir,
	}
}

func (m *Manager) Record(model string, duration time.Duration, success bool, tokensIn, tokensOut int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	date := time.Now().Format("2006-01-02")
	stats := m.loadDailyStats(date)

	// Append Record
	stats.Records = append(stats.Records, RequestRecord{
		Time:      time.Now(),
		Model:     model,
		Duration:  float64(duration.Milliseconds()),
		Success:   success,
		TokensIn:  tokensIn,
		TokensOut: tokensOut,
	})

	// Update Summary
	if stats.Summary == nil {
		stats.Summary = make(map[string]ModelStats)
	}

	s := stats.Summary[model]
	s.Requests++
	s.TokensIn += tokensIn
	s.TokensOut += tokensOut
	stats.Summary[model] = s

	stats.TotalReq++

	m.saveDailyStats(stats)
}

func (m *Manager) GetDaily(date string) *DailyStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loadDailyStats(date)
}

func (m *Manager) loadDailyStats(date string) *DailyStats {
	path := filepath.Join(m.dataDir, date+".json")
	stats := &DailyStats{Date: date, Summary: make(map[string]ModelStats)}

	bytes, err := os.ReadFile(path)
	if err == nil {
		json.Unmarshal(bytes, stats)
	}
	return stats
}

func (m *Manager) saveDailyStats(stats *DailyStats) {
	path := filepath.Join(m.dataDir, stats.Date+".json")
	bytes, _ := json.MarshalIndent(stats, "", "  ")
	os.WriteFile(path, bytes, 0644)
}
