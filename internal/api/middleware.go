package api

import (
	"strings"

	"qiservice/internal/auth"
	"qiservice/internal/db"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware - Parses JWT Token or API Key
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. Try JWT (Authorization: Bearer <token>)
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
			tokenString := strings.TrimPrefix(authHeader, "Bearer ")
			claims, err := auth.ParseToken(tokenString)
			if err == nil {
				// Valid JWT
				c.Set("userID", claims.UserID)
				c.Set("username", claims.Username)
				c.Set("role", claims.Role)
				c.Next()
				return
			}
		}

		// 2. Try API Key (x-api-key or Bearer sk-...)
		// Only for Service invocations, not for Admin API.
		// If accessing /api/chat/completions, allow API Key.
		// If accessing /api/users, STRICTLY require JWT.

		if strings.HasPrefix(c.Request.URL.Path, "/api/users") ||
			strings.HasPrefix(c.Request.URL.Path, "/api/services") ||
			strings.HasPrefix(c.Request.URL.Path, "/api/stats") {
			c.AbortWithStatusJSON(401, gin.H{"error": "Authentication required (JWT)"})
			return
		}

		// Legacy API Key Logic for Chat/Completions
		apiKey := c.GetHeader("x-api-key")
		if apiKey == "" {
			if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
				apiKey = strings.TrimPrefix(authHeader, "Bearer ")
			}
		}

		if apiKey != "" {
			var keyRecord db.APIKey
			if err := db.DB.Preload("User").Where("key = ? AND is_active = ?", apiKey, true).First(&keyRecord).Error; err == nil {
				if keyRecord.User.ID != 0 {
					// Check Quota
					u := keyRecord.User
					// Quota < 0 means Unlimited. Quota >= 0 means Limited.
					if u.Role != db.RoleSuperAdmin && u.Role != db.RoleAdmin && u.Quota >= 0 && u.UsedAmount >= u.Quota {
						c.AbortWithStatusJSON(403, gin.H{"error": "Quota exceeded"})
						return
					}
					c.Set("userID", u.ID)
					c.Set("username", u.Username)
					c.Set("role", u.Role) // API Key inherits User Role
					c.Next()
					return
				}
			}
		}

		c.AbortWithStatusJSON(401, gin.H{"error": "Unauthorized"})
	}
}

// RoleMiddleware - Enforces Role Access
func RoleMiddleware(allowedRoles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userRole := c.GetString("role")
		for _, role := range allowedRoles {
			if userRole == role {
				c.Next()
				return
			}
		}
		c.AbortWithStatusJSON(403, gin.H{"error": "Forbidden: Insufficient Permissions"})
	}
}
