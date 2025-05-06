package config

import (
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"log"
	"time"
	"github.com/gofiber/fiber/v2/middleware/session"
	"golang.org/x/crypto/bcrypt"
)

const QueueName = "captcha_tasks"

var (
	// Подключение к БД
	DB *sql.DB

	// Хранилище сессий
	Store = session.New()
)

// Создание дефолтного админа, если пользователей нет
func CreateDefaultAdmin() {
	var count int
	err := DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		log.Printf("Ошибка проверки пользователей: %v", err)
		return
	}
	if count > 0 {
		return
	}
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	now := time.Now()
	_, err = DB.Exec("INSERT INTO users (username, password_hash, role, created_at) VALUES (?, ?, ?, ?)",
		"admin", string(passwordHash), "admin", now)
	if err != nil {
		log.Printf("Ошибка создания администратора: %v", err)
		return
	}
	log.Println("Created default admin user: admin/admin123")
}
