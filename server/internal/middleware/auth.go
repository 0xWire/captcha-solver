package middleware

import (
	"captcha-solver/internal/config"
	"captcha-solver/internal/models"
	"database/sql"
	"log"
	"strconv"

	"github.com/gofiber/fiber/v2"
	_ "github.com/mattn/go-sqlite3"
)

// AuthRequest for API auth
type AuthRequest struct {
	ApiKey string `json:"api_key"`
}

// Middleware API аутентификации – только для клиентов
func AuthMiddleware(c *fiber.Ctx) error {
	sess, err := config.Store.Get(c)
	if err != nil {
		return c.Redirect("/login")
	}

	userIDRaw := sess.Get("userID")
	if userIDRaw == nil {
		return c.Redirect("/login")
	}

	var userID int64
	switch v := userIDRaw.(type) {
	case int64:
		userID = v
	case int:
		userID = int64(v)
	case string:
		userID, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			sess.Destroy()
			return c.Redirect("/login")
		}
	default:
		sess.Destroy()
		return c.Redirect("/login")
	}

	// Use sql.NullString and sql.NullFloat64 for nullable fields
	var (
		user         models.User
		apiKeyDB     sql.NullString
		balanceDB    sql.NullFloat64
		passwordHash sql.NullString
	)

	err = config.DB.QueryRow("SELECT id, username, password_hash, role, api_key, balance, created_at FROM users WHERE id = ?", userID).
		Scan(&user.ID, &user.Username, &passwordHash, &user.Role, &apiKeyDB, &balanceDB, &user.CreatedAt)

	if err != nil {
		sess.Destroy()
		return c.Redirect("/login")
	}

	if passwordHash.Valid {
		user.PasswordHash = passwordHash.String
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

// APIKeyMiddleware перевіряє API ключ в заголовку запиту
func APIKeyMiddleware(c *fiber.Ctx) error {
	log.Printf("🔍 Перевірка API ключа для шляху: %s", c.Path())
	log.Printf("📝 Заголовки запиту: %v", c.GetReqHeaders())

	apiKey := c.Get("X-API-Key")
	if apiKey == "" {
		log.Printf("❌ Відсутній API ключ в заголовку")
		return c.Status(401).JSON(fiber.Map{
			"status":  "error",
			"message": "API key is required",
		})
	}

	log.Printf("🔑 Отримано API ключ: %s", apiKey)

	var user models.User
	err := config.DB.QueryRow("SELECT id, username, role FROM users WHERE api_key = ?", apiKey).
		Scan(&user.ID, &user.Username, &user.Role)

	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("❌ Неправильний API ключ: %s", apiKey)
			return c.Status(401).JSON(fiber.Map{
				"status":  "error",
				"message": "Invalid API key",
			})
		}
		log.Printf("❌ Помилка бази даних при перевірці API ключа: %v", err)
		return c.Status(500).JSON(fiber.Map{
			"status":  "error",
			"message": "Server error during authentication",
		})
	}

	log.Printf("✅ Користувач авторизований через API ключ: %s (ID: %d, роль: %s)", user.Username, user.ID, user.Role)

	// Зберігаємо користувача в контексті для подальшого використання
	c.Locals("user", &user)
	return c.Next()
}
