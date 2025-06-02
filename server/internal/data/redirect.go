package data

import (
	"captcha-solver/internal/config"
	"captcha-solver/internal/models"
	"database/sql"
	"log"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	_ "github.com/mattn/go-sqlite3"
)

// Root redirection based on role
func RootRedirect(app *fiber.App) {
	app.Get("/", func(c *fiber.Ctx) error {
		sess, err := config.Store.Get(c)
		if err != nil || sess.Get("userID") == nil {
			return c.Redirect("/login")
		}

		userIDRaw := sess.Get("userID")
		var userID int64
		switch v := userIDRaw.(type) {
		case int64:
			userID = v
		case int:
			userID = int64(v)
		case string:
			userID, err = strconv.ParseInt(v, 10, 64)
			if err != nil {
				return c.Redirect("/login")
			}
		default:
			return c.Redirect("/login")
		}

		var user models.User
		var apiKeyDB sql.NullString
		var balanceDB sql.NullFloat64
		err = config.DB.QueryRow("SELECT id, username, role, api_key, balance, created_at FROM users WHERE id = ?", userID).
			Scan(&user.ID, &user.Username, &user.Role, &apiKeyDB, &balanceDB, &user.CreatedAt)
		if err != nil {
			sess.Destroy()
			return c.Redirect("/login")
		}
		if apiKeyDB.Valid {
			user.APIKey = apiKeyDB.String
		}
		if balanceDB.Valid {
			user.Balance = balanceDB.Float64
		}

		log.Printf("Root redirect for user: %s with role: %s, API Key: %s, Balance: %.2f",
			user.Username, user.Role, user.APIKey, user.Balance)

		switch strings.ToLower(user.Role) {
		case "admin":
			return c.Redirect("/admin")
		case "worker":
			return c.Redirect("/worker/solve-queue")
		case "client":
			return c.Redirect("/client")
		default:
			return c.Redirect("/login")
		}
	})
}
