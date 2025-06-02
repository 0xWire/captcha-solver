package handlers

import (
	"captcha-solver/internal/config"
	"captcha-solver/internal/models"
	"captcha-solver/internal/utils"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"
)

func ShowAdminDashboard(c *fiber.Ctx) error {
	return c.Render("admin/dashboard", fiber.Map{
		"Title": "Admin Dashboard",
		"User":  c.Locals("user").(*models.User),
	}, "layout")
}

func ShowUsersAdmin(c *fiber.Ctx) error {
	rows, err := config.DB.Query("SELECT id, username, role, api_key, created_at FROM users")
	if err != nil {
		return c.Status(500).SendString("Error getting users")
	}
	defer rows.Close()

	var userList []*models.User
	for rows.Next() {
		var user models.User
		if err := rows.Scan(&user.ID, &user.Username, &user.Role, &user.APIKey, &user.CreatedAt); err != nil {
			continue
		}
		userList = append(userList, &user)
	}
	return c.Render("admin/users", fiber.Map{
		"Title": "Manage Users",
		"User":  c.Locals("user").(*models.User),
		"Users": userList,
	}, "layout")
}

// Создание пользователя (только администратор)
func CreateUser(c *fiber.Ctx) error {
	username := c.FormValue("username")
	password := c.FormValue("password")
	role := c.FormValue("role")

	if username == "" || password == "" || role == "" {
		return c.Status(400).SendString("Все поля обязательны")
	}

	if role != "admin" && role != "worker" && role != "client" {
		return c.Status(400).SendString("Недопустимая роль")
	}

	var exists int
	err := config.DB.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", username).Scan(&exists)
	if err != nil {
		return c.Status(500).SendString("Ошибка проверки пользователя")
	}
	if exists > 0 {
		return c.Status(400).SendString("Имя пользователя уже занято")
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return c.Status(500).SendString("Ошибка хеширования пароля")
	}

	apiKey := ""
	if role == "client" || role == "worker" {
		if key, err := utils.GenerateAPIKey(); err == nil {
			apiKey = key
		}
	}

	now := time.Now()
	_, err = config.DB.Exec("INSERT INTO users (username, password_hash, role, api_key, created_at) VALUES (?, ?, ?, ?, ?)",
		username, string(passwordHash), role, apiKey, now)
	if err != nil {
		return c.Status(500).SendString("Ошибка создания пользователя")
	}
	return c.Redirect("/admin/users")
}

// Удаление пользователя
func DeleteUser(c *fiber.Ctx) error {
	idParam := c.Params("id")
	var userID int64
	if _, err := fmt.Sscan(idParam, &userID); err != nil {
		return c.Status(400).SendString("Неверный ID пользователя")
	}

	res, err := config.DB.Exec("DELETE FROM users WHERE id = ?", userID)
	if err != nil {
		return c.Status(500).SendString("Ошибка удаления пользователя")
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return c.Status(404).SendString("Пользователь не найден")
	}
	return c.SendString("OK")
}
