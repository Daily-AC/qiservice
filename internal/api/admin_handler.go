package api

import (
	"qiservice/internal/db"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ListUsersHandler - GET /api/users
func ListUsersHandler(c *gin.Context) {
	var users []db.User
	if err := db.DB.Preload("APIKeys").Order("id desc").Find(&users).Error; err != nil {
		c.JSON(500, gin.H{"error": "Failed to fetch users"})
		return
	}
	c.JSON(200, users)
}

// CreateUserRequest
type CreateUserRequest struct {
	Username string  `json:"username" binding:"required"`
	Password string  `json:"password" binding:"required"`
	Role     string  `json:"role"`
	Quota    float64 `json:"quota"`
}

// CreateUserHandler - POST /api/users
func CreateUserHandler(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Simple hash logic (Production usage should use bcrypt)
	// We reuse hashPassword from somewhere?
	// Or just do it here for now as placeholder.
	pwdHash := req.Password // TODO: Real Hash

	// Check Requestor Permissions (Assumes Middleware injects "role")
	requestorRole := c.GetString("role")

	targetRole := db.RoleUser
	if req.Role == db.RoleAdmin {
		if requestorRole != db.RoleSuperAdmin {
			c.JSON(403, gin.H{"error": "Only Super Admin can create Admins"})
			return
		}
		targetRole = db.RoleAdmin
	} else if req.Role == db.RoleSuperAdmin {
		c.JSON(403, gin.H{"error": "Cannot create Super Admin via API"})
		return
	}

	user := db.User{
		Username:     req.Username,
		PasswordHash: pwdHash,
		Role:         targetRole,
		Quota:        req.Quota,
		Balance:      req.Quota, // Initial balance = Quota? Or Balance is remaining?
		// Let's say Quota is Monthly limit, Balance is Credit?
		// For simplicity: UsedAmount vs Quota.
		// Balance concept might be "Prepaid".
		// Let's stick to Quota model: UsedAmount vs Quota.
	}

	if err := db.DB.Create(&user).Error; err != nil {
		c.JSON(500, gin.H{"error": "Failed to create user (username might exist)"})
		return
	}

	c.JSON(200, user)
}

// GenerateAPIKeyRequest
type GenerateAPIKeyRequest struct {
	UserID uint   `json:"user_id" binding:"required"`
	Name   string `json:"name"`
}

// GenerateAPIKeyHandler - POST /api/keys
func GenerateAPIKeyHandler(c *gin.Context) {
	var req GenerateAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Verify User exists
	var user db.User
	if err := db.DB.First(&user, req.UserID).Error; err != nil {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	newKey := "sk-" + strings.ReplaceAll(uuid.New().String(), "-", "")
	apiKey := db.APIKey{
		Key:      newKey,
		Name:     req.Name,
		UserID:   user.ID,
		IsActive: true,
	}

	if err := db.DB.Create(&apiKey).Error; err != nil {
		c.JSON(500, gin.H{"error": "Failed to generate key"})
		return
	}

	c.JSON(200, apiKey)
}

type UpdateUserRoleRequest struct {
	UserID uint   `json:"user_id" binding:"required"`
	Role   string `json:"role" binding:"required"`
}

func UpdateUserRoleHandler(c *gin.Context) {
	var req UpdateUserRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Only SuperAdmin can modify roles? Or Admin can promote User to Admin?
	// Let's stick to strict: SuperAdmin can do anything.
	// Admin can NOT change roles for now.
	requestorRole := c.GetString("role")
	if requestorRole != db.RoleSuperAdmin {
		c.JSON(403, gin.H{"error": "Only Super Admin can update roles"})
		return
	}

	if req.Role != db.RoleAdmin && req.Role != db.RoleUser {
		c.JSON(400, gin.H{"error": "Invalid role"})
		return
	}

	if err := db.DB.Model(&db.User{}).Where("id = ?", req.UserID).Update("role", req.Role).Error; err != nil {
		c.JSON(500, gin.H{"error": "Failed to update user role"})
		return
	}

	c.JSON(200, gin.H{"status": "updated"})
}
