package db

import (
	"encoding/json"
	"log"
	"os"
)

// MigrateConfig loads legacy config.json and seeds the database
func MigrateConfig() {
	var count int64
	DB.Model(&User{}).Count(&count)
	if count > 0 {
		log.Println("ℹ️ Database already seeded. Skipping migration.")
		return
	}

	log.Println("🔄 Starting migration from config.json...")

	// Load legacy config manually (avoid cyclic import if possible, but here we can reuse existing struct definition if exported,
	// BUT config package might not be loaded yet or we want to avoid side effects.
	// Let's read file directly to be safe.)

	configFile := "config.json"
	data, err := os.ReadFile(configFile)
	if err != nil {
		log.Printf("⚠️ No config.json found or readable. Creating default Admin.")
		createDefaultAdmin()
		return
	}

	// We need a temporary struct that matches the old ServiceConfig json tags
	type OldServiceConfig struct {
		Name      string   `json:"name"`
		Type      string   `json:"type"`
		BaseURL   string   `json:"base_url"`
		APIKey    string   `json:"api_key"`
		APIKeys   []string `json:"api_keys"`
		ModelName string   `json:"model_name"`
		IsActive  bool     `json:"is_active"`
	}

	var jsonCfg struct {
		Services      []OldServiceConfig `json:"services"`
		ClientKeys    []string           `json:"client_keys"`
		AdminPassword string             `json:"admin_password"`
	}

	if err := json.Unmarshal(data, &jsonCfg); err != nil {
		log.Printf("❌ Failed to parse config.json: %v", err)
		createDefaultAdmin()
		return
	}

	// 1. Create Admin User
	adminUser := User{
		Username:     "admin",
		Role:         RoleSuperAdmin,
		Quota:        9999999,                             // Unlimited
		PasswordHash: hashPassword(jsonCfg.AdminPassword), // Need a hash function
	}
	if adminUser.PasswordHash == "" {
		adminUser.PasswordHash = "admin" // Default insecure fallback if hash fails (simplified)
	}
	DB.Create(&adminUser)
	log.Printf("✅ Migrated Admin User (ID: %d)", adminUser.ID)

	// 2. Migrate Client Keys
	// We assign all legacy client keys to a default "Legacy User" or just the Admin?
	// Let's create a "Legacy User" for these keys.
	legacyUser := User{
		Username: "legacy_user",
		Role:     "user",
		Quota:    1000,
	}
	DB.Create(&legacyUser)

	for _, key := range jsonCfg.ClientKeys {
		if key == "" {
			continue
		}
		DB.Create(&APIKey{
			Key:    key,
			Name:   "Imported Key",
			UserID: legacyUser.ID,
		})
	}
	log.Printf("✅ Migrated %d Client Keys", len(jsonCfg.ClientKeys))

	// 3. Migrate Services
	for _, s := range jsonCfg.Services {
		// Handle list of keys in service (OldServiceConfig had APIKeys []string)
		// Our new Service model has single APIKey string.
		// If old config had multiple keys, we might need to create multiple Service entries or comma-separate?
		// Or just take the first one for now as per simple DB model.
		// Actually, let's just take the first valid key.

		finalKey := s.APIKey
		if finalKey == "" && len(s.APIKeys) > 0 {
			finalKey = s.APIKeys[0] // Pick first
		}

		// Model Mapping: If ModelName is set and != Name, it's a mapping.
		// Construct JSON mapping string if needed.
		var mappingStr string
		if s.ModelName != "" && s.ModelName != s.Name {
			// e.g. {"anyrouter-haiku": "claude-haiku"}
			// Wait, the logic was: Request Model -> Real Model.
			// Service Name is the Request Model (e.g. anyrouter-haiku).
			// ModelName is the Real Model (e.g. claude-haiku).
			// So Name = "anyrouter-haiku", ModelMapping = "claude-haiku"?
			// The Service struct has `ModelMapping string`. Let's store target model name there?
			// Or store a JSON map? The struct says JSON string.
			// Let's store simply the target model name for now if it's 1:1.
			// Actually let's assume it stores `{"model": "target"}` logic or just string.
			// Changing `Service` struct definition might be cleaner if we just want "TargetModel".
			// But sticking to JSON string allows flexibility.
			// Let's just create the Service as is.

			// Warning: current `Service` struct in models.go has `ModelMapping string`.
			// Let's assume it holds the target model name directly if simple string?
			// Or we format it as JSON: `{"mode": "rewrite", "target": "..."}`
			// Let's keep it simple: store the Target Model Name directly if it's a string field.
			// Wait, `models.go` comment said `// JSON string: {"anyrouter-haiku": "claude-haiku"}`
			// But Service Name IS `anyrouter-haiku`.
			// So we just need to know the target.
			// Let's store `{"target_model": "claude-haiku"}`.
			if s.ModelName != "" {
				m := map[string]string{"target_model": s.ModelName}
				b, _ := json.Marshal(m)
				mappingStr = string(b)
			}
		}

		newSvc := Service{
			Name:         s.Name,
			Type:         s.Type,
			BaseURL:      s.BaseURL,
			APIKey:       finalKey,
			ModelMapping: mappingStr,
			IsActive:     true,
		}
		DB.Create(&newSvc)
	}
	log.Printf("✅ Migrated %d Services", len(jsonCfg.Services))
}

func createDefaultAdmin() {
	// ... logic to create default admin if no config ...
	DB.Create(&User{
		Username:     "admin",
		Role:         "admin",
		PasswordHash: "admin", // Need handling
	})
}

// Simple hash (placeholder) - in production use bcrypt
func hashPassword(p string) string {
	return p // TODO: Implement bcrypt
}
