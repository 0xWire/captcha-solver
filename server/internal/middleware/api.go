package middleware

import (
	"captcha-solver/internal/config"
	"captcha-solver/internal/models"
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"log"
	"github.com/gofiber/fiber/v2"
)

// Add this function to handle API authentication
func ApiAuthMiddleware(c *fiber.Ctx) error {
	// Try multiple methods for API key
	apiKey := c.Get("X-API-Key")
	if apiKey == "" {
		// Check query parameter as fallback
		apiKey = c.Query("api_key")
	}

	if apiKey == "" {
		log.Println("API authentication failed: Missing API key")
		return c.Status(401).JSON(fiber.Map{"error": "API key required"})
	}

	var (
		user      models.User
		apiKeyDB  sql.NullString
		balanceDB sql.NullFloat64
	)

	err := config.DB.QueryRow("SELECT id, username, role, api_key, balance, created_at FROM users WHERE api_key = ?", apiKey).
		Scan(&user.ID, &user.Username, &user.Role, &apiKeyDB, &balanceDB, &user.CreatedAt)

	if err != nil {
		log.Printf("API authentication failed: %v", err)
		return c.Status(401).JSON(fiber.Map{"error": "Invalid API key"})
	}

	if apiKeyDB.Valid {
		user.APIKey = apiKeyDB.String
	}

	if balanceDB.Valid {
		user.Balance = balanceDB.Float64
	}

	c.Locals("user", &user)
	return c.Next()
}
