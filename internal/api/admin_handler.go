package api

import (
	"qiservice/internal/db"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ListUsersHandler - GET /api/users
func ListUsersHandler(c *gin.Context) {
	requestorRole := c.GetString("role")

	var users []db.User
	query := db.DB.Preload("APIKeys").Order("id desc")

	// Filter: Admin can only see Users (or themselves, maybe? Let's just say Users)
	// SuperAdmin sees all.
	if requestorRole == db.RoleAdmin {
		query = query.Where("role = ?", db.RoleUser)
	} else if requestorRole != db.RoleSuperAdmin {
		// Regular user should not be here (Middleware protected), but safety check
		c.JSON(403, gin.H{"error": "Forbidden"})
		return
	}

	if err := query.Find(&users).Error; err != nil {
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

type UpdateUserRequest struct {
	UserID   uint     `json:"user_id" binding:"required"`
	Password string   `json:"password"`
	Quota    *float64 `json:"quota"` // Use pointer to distinguish 0 vs nil, and allow negative
	Role     string   `json:"role"`  // Optional
}

// UpdateUserHandler - POST /api/user_update
func UpdateUserHandler(c *gin.Context) {
	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	requestorRole := c.GetString("role")
	var targetUser db.User
	if err := db.DB.First(&targetUser, req.UserID).Error; err != nil {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	// Permission Check
	// 1. SuperAdmin can update anyone (Role, Quota, Pwd).
	// 2. Admin can ONLY update User (Quota, Pwd). NO Role change.
	if requestorRole == db.RoleAdmin {
		if targetUser.Role != db.RoleUser {
			c.JSON(403, gin.H{"error": "Admin can only manage ordinary Users"})
			return
		}
		if req.Role != "" && req.Role != targetUser.Role {
			c.JSON(403, gin.H{"error": "Admin cannot change roles"})
			return
		}
	} else if requestorRole != db.RoleSuperAdmin {
		c.JSON(403, gin.H{"error": "Forbidden"})
		return
	}

	updates := make(map[string]interface{})
	if req.Quota != nil {
		updates["quota"] = *req.Quota
	}
	if req.Password != "" {
		updates["password_hash"] = req.Password // TODO: Hash
	}
	if req.Role != "" {
		if requestorRole == db.RoleSuperAdmin {
			if req.Role == db.RoleAdmin || req.Role == db.RoleUser {
				updates["role"] = req.Role
			}
		}
	}

	if len(updates) > 0 {
		if err := db.DB.Model(&targetUser).Updates(updates).Error; err != nil {
			c.JSON(500, gin.H{"error": "Failed to update user"})
			return
		}
	}

	c.JSON(200, gin.H{"status": "updated"})
}

// ListMyKeysHandler - GET /api/my_keys
func ListMyKeysHandler(c *gin.Context) {
	userID := c.GetUint("userID")
	var keys []db.APIKey
	if err := db.DB.Where("user_id = ?", userID).Find(&keys).Error; err != nil {
		c.JSON(500, gin.H{"error": "Failed to fetch keys"})
		return
	}
	c.JSON(200, keys)
}

// DeleteUserHandler - DELETE /api/users/:id
func DeleteUserHandler(c *gin.Context) {
	id := c.Param("id")
	requestorRole := c.GetString("role")

	var user db.User
	if err := db.DB.First(&user, id).Error; err != nil {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}

	// Permission: SuperAdmin deletes anyone (except self?), Admin deletes User.
	if requestorRole == db.RoleAdmin {
		if user.Role != db.RoleUser {
			c.JSON(403, gin.H{"error": "Admin can only delete Users"})
			return
		}
	} else if requestorRole != db.RoleSuperAdmin {
		c.JSON(403, gin.H{"error": "Forbidden"})
		return
	}

	// Use Unscoped to verify hard delete (or handle soft delete properly)
	// For this user management, we prefer Hard Delete to allow re-creating same username.
	if err := db.DB.Unscoped().Delete(&user).Error; err != nil {
		c.JSON(500, gin.H{"error": "Failed to delete user"})
		return
	}
	c.JSON(200, gin.H{"status": "deleted"})
}

// DeleteMyKeyHandler - DELETE /api/my_keys/:id
func DeleteMyKeyHandler(c *gin.Context) {
	keyID := c.Param("id")
	userID := c.GetUint("userID")

	var key db.APIKey
	if err := db.DB.Where("id = ? AND user_id = ?", keyID, userID).First(&key).Error; err != nil {
		c.JSON(404, gin.H{"error": "Key not found"})
		return
	}

	if err := db.DB.Delete(&key).Error; err != nil {
		c.JSON(500, gin.H{"error": "Failed to delete key"})
		return
	}
	c.JSON(200, gin.H{"status": "deleted"})
}

// GetMyProfileHandler - GET /api/user/me
func GetMyProfileHandler(c *gin.Context) {
	userID := c.GetUint("userID")
	var user db.User
	if err := db.DB.First(&user, userID).Error; err != nil {
		c.JSON(404, gin.H{"error": "User not found"})
		return
	}
	c.JSON(200, user)
}
