package db

import (
	"time"

	"gorm.io/gorm"
)

// Role Definitions
const (
	RoleSuperAdmin = "super_admin"
	RoleAdmin      = "admin"
	RoleUser       = "user"
)

// User represents a system user (admin or client)
type User struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	Username     string         `gorm:"uniqueIndex;not null" json:"username"`
	PasswordHash string         `json:"-"`                          // Hashed password, not exposed in JSON
	Role         string         `gorm:"default:'user'" json:"role"` // 'super_admin', 'admin', 'user'
	Balance      float64        `gorm:"default:0" json:"balance"`   // Credit balance
	Quota        float64        `gorm:"default:0" json:"quota"`     // Max quota allowed
	UsedAmount   float64        `gorm:"default:0" json:"used_amount"`
	APIKeys      []APIKey       `gorm:"foreignKey:UserID" json:"api_keys,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

// APIKey represents a client verification token
type APIKey struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Key       string    `gorm:"uniqueIndex;not null" json:"key"` // "sk-..."
	Name      string    `json:"name"`                            // "My Laptop", "Testing"
	UserID    uint      `gorm:"index;not null" json:"user_id"`
	User      User      `json:"-"` // Belongs To Relation
	LastUsed  time.Time `json:"last_used"`
	CreatedAt time.Time `json:"created_at"`
	IsActive  bool      `gorm:"default:true" json:"is_active"`
}

// Service represents an Upstream LLM Provider (replaces ServiceConfig)
type Service struct {
	ID           uint   `gorm:"primaryKey" json:"id"`
	Name         string `gorm:"uniqueIndex;not null" json:"name"` // "claude-3-5-sonnet"
	Type         string `json:"type"`                             // "openai", "anthropic", "gemini"
	BaseURL      string `json:"base_url"`
	APIKey       string `json:"api_key"`       // Upstream Key (Legacy/Primary)
	APIKeys      string `json:"api_keys_json"` // JSON Array of keys: ["sk-...", "sk-..."]
	ModelMapping string `json:"model_mapping"` // JSON string: {"anyrouter-haiku": "claude-haiku"}
	IsActive     bool   `gorm:"default:true" json:"is_active"`
}

// RequestLog stores usage statistics (replaces file-based stats)
type RequestLog struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	UserID           uint      `gorm:"index" json:"user_id"`
	ServiceModel     string    `gorm:"index" json:"model"` // The model name requested
	UpstreamModel    string    `json:"upstream_model"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	DurationMs       int64     `json:"duration_ms"`
	Status           int       `json:"status"` // HTTP Status Code (200, 500, etc)
	CreatedAt        time.Time `gorm:"index" json:"created_at"`
}
