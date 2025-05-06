package middleware

import (
	"captcha-solver/internal/config"
	"captcha-solver/internal/models"
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"strconv"
	"github.com/gofiber/fiber/v2"
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
