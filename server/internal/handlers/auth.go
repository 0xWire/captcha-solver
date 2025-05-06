package handlers

import (
	"captcha-solver/internal/config"
	"captcha-solver/internal/models"
	"captcha-solver/internal/utils"
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"log"
	"time"
	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"
)

// Страница входа
func ShowLoginPage(c *fiber.Ctx) error {
	return c.Render("login", fiber.Map{
		"Title": "Вход в систему",
	}, "layout")
}

// Страница регистрации
func ShowRegisterPage(c *fiber.Ctx) error {
	return c.Render("register", fiber.Map{
		"Title": "Регистрация",
	}, "layout")
}

// Обработка входа
func HandleLogin(c *fiber.Ctx) error {
	username := c.FormValue("username")
	password := c.FormValue("password")

	if username == "" || password == "" {
		return c.Status(400).SendString("Имя пользователя и пароль обязательны")
	}

	var (
		user    models.User
		apiKey  sql.NullString
		balance sql.NullFloat64
	)

	err := config.DB.QueryRow("SELECT id, username, password_hash, role, api_key, balance, created_at FROM users WHERE username = ?", username).
		Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role, &apiKey, &balance, &user.CreatedAt)

	if err != nil {
		log.Printf("Login error for %s: %v", username, err)
		return c.Status(400).SendString("Неверное имя пользователя или пароль")
	}

	// Only set the API key if it's not NULL
	if apiKey.Valid {
		user.APIKey = apiKey.String
	}

	// Only set the balance if it's not NULL
	if balance.Valid {
		user.Balance = balance.Float64
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		log.Printf("Password mismatch for %s", username)
		return c.Status(400).SendString("Неверное имя пользователя или пароль")
	}

	sess, err := config.Store.Get(c)
	if err != nil {
		return c.Status(500).SendString("Ошибка сессии")
	}

	sess.Set("userID", user.ID)
	if err := sess.Save(); err != nil {
		return c.Status(500).SendString("Ошибка сохранения сессии")
	}

	switch user.Role {
	case "admin":
		return c.Redirect("/admin")
	case "worker":
		return c.Redirect("/worker/solve-queue")
	case "client":
		return c.Redirect("/client")
	default:
		return c.Redirect("/")
	}
}

// Обработка регистрации
func HandleRegister(c *fiber.Ctx) error {
	username := c.FormValue("username")
	password := c.FormValue("password")
	role := "client" // Default role for self-registration

	if username == "" || password == "" {
		return c.Status(400).SendString("Имя пользователя и пароль обязательны")
	}

	// Check if username already exists
	var exists bool
	err := config.DB.QueryRow("SELECT 1 FROM users WHERE username = ?", username).Scan(&exists)
	if err == nil {
		return c.Status(400).SendString("Имя пользователя уже занято")
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("Password hash error: %v", err)
		return c.Status(500).SendString("Ошибка хеширования пароля")
	}

	apiKey, err := utils.GenerateAPIKey()
	if err != nil {
		log.Printf("API key generation error: %v", err)
		return c.Status(500).SendString("Ошибка генерации API ключа")
	}

	// Insert the new user with balance field included
	_, err = config.DB.Exec("INSERT INTO users (username, password_hash, role, api_key, balance, created_at) VALUES (?, ?, ?, ?, 0, ?)",
		username, passwordHash, role, apiKey, time.Now())

	if err != nil {
		log.Printf("User creation error: %v", err)
		return c.Status(500).SendString("Ошибка создания пользователя")
	}

	return c.Redirect("/login")
}

// Выход
func HandleLogout(c *fiber.Ctx) error {
	sess, err := config.Store.Get(c)
	if err == nil {
		sess.Destroy()
	}
	return c.Redirect("/login")
}
