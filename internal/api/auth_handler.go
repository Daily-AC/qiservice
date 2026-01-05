package api

import (
	"qiservice/internal/auth"
	"qiservice/internal/db"

	"github.com/gin-gonic/gin"
)

type RegisterRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required,min=6"`
}

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// RegisterHandler - POST /api/register
func RegisterHandler(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// Check if user exists
	var count int64
	db.DB.Model(&db.User{}).Where("username = ?", req.Username).Count(&count)
	if count > 0 {
		c.JSON(409, gin.H{"error": "Username already exists"})
		return
	}

	// Create User (Default Role: User)
	newUser := db.User{
		Username:     req.Username,
		PasswordHash: req.Password, // TODO: Use BCrypt in production
		Role:         db.RoleUser,
		Quota:        100000, // Default Quota
		Balance:      0,
	}

	if err := db.DB.Create(&newUser).Error; err != nil {
		c.JSON(500, gin.H{"error": "Failed to create user"})
		return
	}

	c.JSON(201, gin.H{"message": "User registered successfully"})
}

// LoginHandler - POST /api/login (Replaces old admin login)
func UserLoginHandler(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	var user db.User
	if err := db.DB.Where("username = ? AND password_hash = ?", req.Username, req.Password).First(&user).Error; err != nil {
		c.JSON(401, gin.H{"error": "Invalid username or password"})
		return
	}

	// Generate JWT
	token, err := auth.GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to generate token"})
		return
	}

	c.JSON(200, gin.H{
		"token": token,
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
			"role":     user.Role,
		},
	})
}
