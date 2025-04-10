package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
	"github.com/gofiber/template/html/v2"
	"github.com/gofiber/websocket/v2"
	amqp "github.com/rabbitmq/amqp091-go"
	"golang.org/x/crypto/bcrypt"

	_ "github.com/mattn/go-sqlite3"
)

// User представляет пользователя
type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"` // не выводится в JSON
	Role         string    `json:"role"`
	APIKey       string    `json:"api_key,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	Balance      float64   `json:"balance"`
}

// CaptchaTask описывает задачу по решению капчи
type CaptchaTask struct {
	ID              int64  `json:"id"`
	UserID          int64  `json:"user_id"`             // пользователь, отправивший задачу
	SolverID        int64  `json:"solver_id,omitempty"` // пользователь, решивший задачу (если есть)
	CaptchaType     string `json:"captcha_type"`
	SiteKey         string `json:"sitekey"`
	TargetURL       string `json:"target_url"`
	CaptchaResponse string `json:"captcha_response,omitempty"`
}

// AuthRequest for API auth
type AuthRequest struct {
	ApiKey string `json:"api_key"`
}

// Task is the simple task structure for WebSocket communication
type Task struct {
	Type    string `json:"type"`
	SiteKey string `json:"sitekey"`
	URL     string `json:"url"`
}

var (
	// Подключение к БД
	db *sql.DB

	// Хранилище сессий
	store = session.New()

	// RabbitMQ
	rabbitMQConn    *amqp.Connection
	rabbitMQChannel *amqp.Channel
)

const queueName = "captcha_tasks"

func main() {
	var err error

	// Connect to RabbitMQ
	rabbitMQConn, err = amqp.Dial("amqp://guest:guest@localhost:5672/")
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer rabbitMQConn.Close()
	rabbitMQChannel, err = rabbitMQConn.Channel()
	if err != nil {
		log.Fatalf("Failed to open RabbitMQ channel: %v", err)
	}
	defer rabbitMQChannel.Close()
	_, err = rabbitMQChannel.QueueDeclare(
		queueName, // queue name
		true,      // durable
		false,     // delete when unused
		false,     // exclusive
		false,     // no-wait
		nil,       // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare queue: %v", err)
	}

	// Open or create the SQLite database
	db, err = sql.Open("sqlite3", "./app.db")
	if err != nil {
		log.Fatalf("Error opening DB: %v", err)
	}
	defer db.Close()
	if err := createTables(); err != nil {
		log.Fatalf("Error creating tables: %v", err)
	}
	createDefaultAdmin()

	// Start RabbitMQ consumer in a goroutine
	go consumeTasks()

	// Initialize HTML template engine (templates in folder views)
	engine := html.New("./views", ".html")
	app := fiber.New(fiber.Config{
		Views: engine,
	})

	// Public routes
	app.Get("/login", showLoginPage)
	app.Post("/login", handleLogin)
	app.Get("/register", showRegisterPage)
	app.Post("/register", handleRegister)
	app.Get("/logout", handleLogout)
	app.Get("/logout", handleLogout)

	// Add websocket route with auth check
	app.Get("/socket", websocket.New(handleWebSocket))
	
	// Add auth endpoint for electron app
	app.Post("/auth", handleSimpleAuth)

	// API routes (with API authentication middleware)
	app.Post("/api/task", apiAuthMiddleware, createTask)
	app.Get("/api/tasks", apiAuthMiddleware, getTasks)
	app.Get("/api/task/:id", apiAuthMiddleware, getTask)

	// Protected routes – requires session authentication
	authGroup := app.Group("/", authMiddleware)
	authGroup.Get("/result/:id", showResult)

	// Admin routes
	adminGroup := authGroup.Group("/admin", roleMiddleware("admin"))
	adminGroup.Get("/", showAdminDashboard)
	adminGroup.Get("/users", showUsers)
	adminGroup.Post("/users", createUser)
	adminGroup.Delete("/users/:id", deleteUser)
	adminGroup.Get("/tasks", showTaskList)

	// Worker routes (with prefix /worker)
	workerGroup := authGroup.Group("/worker", roleMiddleware("admin", "worker"))
	workerGroup.Get("/solve-queue", showSolveQueue)
	workerGroup.Get("/captcha/:id", showCaptcha)
	workerGroup.Post("/solve/:id", handleCaptchaSolution)
	workerGroup.Get("/tasks", showTaskList)

	// Client routes (with prefix /client)
	clientGroup := authGroup.Group("/client", roleMiddleware("admin", "client"))
	clientGroup.Get("/", showClientDashboard)
	clientGroup.Get("/api-key/regenerate", regenerateAPIKey)

	// Shared API endpoints
	authGroup.Get("/api/next-task", getNextTask)
	authGroup.Get("/api/queue-count", getQueueCount)

	// Root redirection based on role
	app.Get("/", func(c *fiber.Ctx) error {
		sess, err := store.Get(c)
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

		var user User
		var apiKeyDB sql.NullString
		var balanceDB sql.NullFloat64
		err = db.QueryRow("SELECT id, username, role, api_key, balance, created_at FROM users WHERE id = ?", userID).
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

		log.Printf("Root redirect for user: %s with role: %s", user.Username, user.Role)

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

	log.Println("Server running on http://localhost:3058")
	log.Fatal(app.Listen(":8090"))
}

func createTables() error {
	// Create users table.
	_, err := db.Exec(`
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL,
		api_key TEXT,
		balance REAL NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL
	)
	`)
	if err != nil {
		return err
	}

	// Create tasks table.
	_, err = db.Exec(`
	CREATE TABLE IF NOT EXISTS tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		solver_id INTEGER,
		captcha_type TEXT NOT NULL,
		sitekey TEXT NOT NULL,
		target_url TEXT NOT NULL,
		captcha_response TEXT,
		created_at DATETIME NOT NULL,
		FOREIGN KEY(user_id) REFERENCES users(id),
		FOREIGN KEY(solver_id) REFERENCES users(id)
	)
	`)
	if err != nil {
		return err
	}

	// Check if the tasks table has the created_at column.
	rows, err := db.Query("PRAGMA table_info(tasks)")
	if err != nil {
		return err
	}
	defer rows.Close()

	hasCreatedAt := false
	for rows.Next() {
		var cid int
		var name string
		var ctype string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return err
		}
		if name == "created_at" {
			hasCreatedAt = true
			break
		}
	}

	// If created_at column does not exist, alter the table.
	if !hasCreatedAt {
		_, err = db.Exec("ALTER TABLE tasks ADD COLUMN created_at DATETIME NOT NULL DEFAULT (datetime('now'))")
		if err != nil {
			return err
		}
	}

	return nil
}

// Middleware для проверки ролей
func roleMiddleware(roles ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*User)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).Redirect("/login")
		}

		// Debug log to see what's happening
		log.Printf("Access check: User %s with role '%s' accessing area requiring roles: %v",
			user.Username, user.Role, roles)

		for _, role := range roles {
			if user.Role == role {
				return c.Next()
			}
		}

		return c.Status(fiber.StatusForbidden).SendString("Доступ запрещен")
	}
}

// Генерация безопасного API ключа
func generateAPIKey() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// Создание дефолтного админа, если пользователей нет
func createDefaultAdmin() {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		log.Printf("Ошибка проверки пользователей: %v", err)
		return
	}
	if count > 0 {
		return
	}
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	now := time.Now()
	_, err = db.Exec("INSERT INTO users (username, password_hash, role, created_at) VALUES (?, ?, ?, ?)",
		"admin", string(passwordHash), "admin", now)
	if err != nil {
		log.Printf("Ошибка создания администратора: %v", err)
		return
	}
	log.Println("Created default admin user: admin/admin123")
}

// Страница входа
func showLoginPage(c *fiber.Ctx) error {
	return c.Render("login", fiber.Map{
		"Title": "Вход в систему",
	}, "layout")
}

// Обработка входа
func handleLogin(c *fiber.Ctx) error {
	username := c.FormValue("username")
	password := c.FormValue("password")

	if username == "" || password == "" {
		return c.Status(400).SendString("Имя пользователя и пароль обязательны")
	}

	var (
		user    User
		apiKey  sql.NullString
		balance sql.NullFloat64
	)

	err := db.QueryRow("SELECT id, username, password_hash, role, api_key, balance, created_at FROM users WHERE username = ?", username).
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

	sess, err := store.Get(c)
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

// Страница регистрации
func showRegisterPage(c *fiber.Ctx) error {
	return c.Render("register", fiber.Map{
		"Title": "Регистрация",
	}, "layout")
}

func showResult(c *fiber.Ctx) error {
	idParam := c.Params("id")
	var taskID int64
	if _, err := fmt.Sscan(idParam, &taskID); err != nil {
		return c.Status(400).SendString("Invalid task ID")
	}

	var task CaptchaTask
	err := db.QueryRow("SELECT id, user_id, solver_id, captcha_type, sitekey, target_url, captcha_response FROM tasks WHERE id = ?", taskID).
		Scan(&task.ID, &task.UserID, &task.SolverID, &task.CaptchaType, &task.SiteKey, &task.TargetURL, &task.CaptchaResponse)
	if err != nil {
		return c.Status(404).SendString("Task not found")
	}

	// Render a result view (you can reuse an existing template or create a new one)
	return c.Render("result", fiber.Map{
		"Title": "Task Result",
		"Task":  task,
		"User":  c.Locals("user").(*User),
	}, "layout")
}

// Обработка регистрации
func handleRegister(c *fiber.Ctx) error {
	username := c.FormValue("username")
	password := c.FormValue("password")
	role := "client" // Default role for self-registration

	if username == "" || password == "" {
		return c.Status(400).SendString("Имя пользователя и пароль обязательны")
	}

	// Check if username already exists
	var exists bool
	err := db.QueryRow("SELECT 1 FROM users WHERE username = ?", username).Scan(&exists)
	if err == nil {
		return c.Status(400).SendString("Имя пользователя уже занято")
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("Password hash error: %v", err)
		return c.Status(500).SendString("Ошибка хеширования пароля")
	}

	apiKey, err := generateAPIKey()
	if err != nil {
		log.Printf("API key generation error: %v", err)
		return c.Status(500).SendString("Ошибка генерации API ключа")
	}

	// Insert the new user with balance field included
	_, err = db.Exec("INSERT INTO users (username, password_hash, role, api_key, balance, created_at) VALUES (?, ?, ?, ?, 0, ?)",
		username, passwordHash, role, apiKey, time.Now())

	if err != nil {
		log.Printf("User creation error: %v", err)
		return c.Status(500).SendString("Ошибка создания пользователя")
	}

	return c.Redirect("/login")
}

// Выход
func handleLogout(c *fiber.Ctx) error {
	sess, err := store.Get(c)
	if err == nil {
		sess.Destroy()
	}
	return c.Redirect("/login")
}

// Панель администратора
func showAdminDashboard(c *fiber.Ctx) error {
	return c.Render("admin/dashboard", fiber.Map{
		"Title": "Панель администратора",
		"User":  c.Locals("user").(*User),
	}, "layout")
}

// Список пользователей для администратора
func showUsers(c *fiber.Ctx) error {
	rows, err := db.Query("SELECT id, username, role, api_key, created_at FROM users")
	if err != nil {
		return c.Status(500).SendString("Ошибка получения пользователей")
	}
	defer rows.Close()

	var userList []*User
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.ID, &user.Username, &user.Role, &user.APIKey, &user.CreatedAt); err != nil {
			continue
		}
		userList = append(userList, &user)
	}
	return c.Render("admin/users", fiber.Map{
		"Title": "Управление пользователями",
		"User":  c.Locals("user").(*User),
		"Users": userList,
	}, "layout")
}

// Создание пользователя (только администратор)
func createUser(c *fiber.Ctx) error {
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
	err := db.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", username).Scan(&exists)
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
	if role == "client" {
		if key, err := generateAPIKey(); err == nil {
			apiKey = key
		}
	}

	now := time.Now()
	_, err = db.Exec("INSERT INTO users (username, password_hash, role, api_key, created_at) VALUES (?, ?, ?, ?, ?)",
		username, string(passwordHash), role, apiKey, now)
	if err != nil {
		return c.Status(500).SendString("Ошибка создания пользователя")
	}
	return c.Redirect("/admin/users")
}

// Удаление пользователя
func deleteUser(c *fiber.Ctx) error {
	idParam := c.Params("id")
	var userID int64
	if _, err := fmt.Sscan(idParam, &userID); err != nil {
		return c.Status(400).SendString("Неверный ID пользователя")
	}

	res, err := db.Exec("DELETE FROM users WHERE id = ?", userID)
	if err != nil {
		return c.Status(500).SendString("Ошибка удаления пользователя")
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return c.Status(404).SendString("Пользователь не найден")
	}
	return c.SendString("OK")
}

// Личный кабинет клиента
func showClientDashboard(c *fiber.Ctx) error {
	user := c.Locals("user").(*User)

	rows, err := db.Query("SELECT id, user_id, solver_id, captcha_type, sitekey, target_url, captcha_response FROM tasks WHERE user_id = ?", user.ID)
	if err != nil {
		return c.Status(500).SendString("Ошибка получения задач")
	}
	defer rows.Close()

	var clientTasks []*CaptchaTask
	for rows.Next() {
		var task CaptchaTask
		if err := rows.Scan(&task.ID, &task.UserID, &task.SolverID, &task.CaptchaType, &task.SiteKey, &task.TargetURL, &task.CaptchaResponse); err != nil {
			continue
		}
		clientTasks = append(clientTasks, &task)
	}

	return c.Render("client/dashboard", fiber.Map{
		"Title": "Личный кабинет",
		"User":  user,
		"Tasks": clientTasks,
	}, "layout")
}

// Обновление (регенерация) API ключа
func regenerateAPIKey(c *fiber.Ctx) error {
	user := c.Locals("user").(*User)
	apiKey, err := generateAPIKey()
	if err != nil {
		return c.Status(500).SendString("Ошибка генерации API ключа")
	}

	_, err = db.Exec("UPDATE users SET api_key = ? WHERE id = ?", apiKey, user.ID)
	if err != nil {
		return c.Status(500).SendString("Ошибка обновления API ключа")
	}
	user.APIKey = apiKey
	return c.JSON(fiber.Map{
		"api_key": apiKey,
	})
}

// Add this function to handle API authentication
func apiAuthMiddleware(c *fiber.Ctx) error {
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
		user      User
		apiKeyDB  sql.NullString
		balanceDB sql.NullFloat64
	)

	err := db.QueryRow("SELECT id, username, role, api_key, balance, created_at FROM users WHERE api_key = ?", apiKey).
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

// Middleware API аутентификации – только для клиентов
func authMiddleware(c *fiber.Ctx) error {
	sess, err := store.Get(c)
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
		user         User
		apiKeyDB     sql.NullString
		balanceDB    sql.NullFloat64
		passwordHash sql.NullString
	)

	err = db.QueryRow("SELECT id, username, password_hash, role, api_key, balance, created_at FROM users WHERE id = ?", userID).
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

// Создание задачи через API
func createTask(c *fiber.Ctx) error {
	user := c.Locals("user").(*User)
	log.Printf("Creating task for user: %s (ID: %d)", user.Username, user.ID)

	type RequestPayload struct {
		SiteKey     string `json:"sitekey"`
		TargetURL   string `json:"target_url"`
		CaptchaType string `json:"captcha_type"`
	}

	var payload RequestPayload
	if err := c.BodyParser(&payload); err != nil {
		log.Printf("Error parsing request body: %v", err)
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request format"})
	}

	if payload.SiteKey == "" || payload.TargetURL == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Sitekey and target URL are required"})
	}

	if payload.CaptchaType == "" {
		payload.CaptchaType = "hcaptcha" // Default type
	}

	// Insert task with proper timestamp
	res, err := db.Exec("INSERT INTO tasks (user_id, captcha_type, sitekey, target_url, created_at) VALUES (?, ?, ?, ?, ?)",
		user.ID, payload.CaptchaType, payload.SiteKey, payload.TargetURL, time.Now())
	if err != nil {
		log.Printf("Database error when creating task: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to create task"})
	}

	taskID, _ := res.LastInsertId()
	task := &CaptchaTask{
		ID:          taskID,
		UserID:      user.ID,
		CaptchaType: payload.CaptchaType,
		SiteKey:     payload.SiteKey,
		TargetURL:   payload.TargetURL,
	}

	// Send to RabbitMQ
	taskBytes, err := json.Marshal(task)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to process task"})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = rabbitMQChannel.PublishWithContext(ctx,
		"",        // exchange
		queueName, // routing key
		false,     // mandatory
		false,     // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        taskBytes,
		})
	if err != nil {
		log.Printf("Error publishing to RabbitMQ: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to queue task"})
	}

	return c.JSON(task)
}

// Получение всех задач для клиента
func getTasks(c *fiber.Ctx) error {
	user := c.Locals("user").(*User)

	rows, err := db.Query("SELECT id, user_id, solver_id, captcha_type, sitekey, target_url, captcha_response FROM tasks WHERE user_id = ?", user.ID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to retrieve tasks"})
	}
	defer rows.Close()

	var tasksList []*CaptchaTask
	for rows.Next() {
		var task CaptchaTask
		if err := rows.Scan(&task.ID, &task.UserID, &task.SolverID, &task.CaptchaType, &task.SiteKey, &task.TargetURL, &task.CaptchaResponse); err != nil {
			continue
		}
		tasksList = append(tasksList, &task)
	}
	return c.JSON(tasksList)
}

// Получение конкретной задачи для клиента
func getTask(c *fiber.Ctx) error {
	user := c.Locals("user").(*User)
	idParam := c.Params("id")
	var taskID int64
	if _, err := fmt.Sscan(idParam, &taskID); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid task ID"})
	}

	var task CaptchaTask
	err := db.QueryRow("SELECT id, user_id, solver_id, captcha_type, sitekey, target_url, captcha_response FROM tasks WHERE id = ? AND user_id = ?", taskID, user.ID).
		Scan(&task.ID, &task.UserID, &task.SolverID, &task.CaptchaType, &task.SiteKey, &task.TargetURL, &task.CaptchaResponse)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "Task not found"})
	}

	return c.JSON(task)
}

func showTaskList(c *fiber.Ctx) error {
	rows, err := db.Query("SELECT id, user_id, solver_id, captcha_type, sitekey, target_url, captcha_response FROM tasks")
	if err != nil {
		return c.Status(500).SendString("Ошибка получения задач")
	}
	defer rows.Close()

	var tasks []*CaptchaTask
	for rows.Next() {
		var task CaptchaTask
		if err := rows.Scan(&task.ID, &task.UserID, &task.SolverID, &task.CaptchaType, &task.SiteKey, &task.TargetURL, &task.CaptchaResponse); err != nil {
			continue
		}
		tasks = append(tasks, &task)
	}
	return c.Render("index", fiber.Map{
		"User":  c.Locals("user").(*User),
		"Tasks": tasks,
	}, "layout")
}

// Очередь задач для решения (workers)
func showSolveQueue(c *fiber.Ctx) error {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM tasks WHERE captcha_response IS NULL OR captcha_response = ''").Scan(&count)
	if err != nil {
		count = 0
	}

	return c.Render("solve-queue", fiber.Map{
		"Title": "Решение капч",
		"Count": count,
		"User":  c.Locals("user").(*User),
	}, "layout")
}

// Страница решения капчи
func showCaptcha(c *fiber.Ctx) error {
	idParam := c.Params("id")
	var taskID int64
	if _, err := fmt.Sscan(idParam, &taskID); err != nil {
		return c.Status(400).SendString("Неверный ID задачи")
	}

	var task CaptchaTask
	err := db.QueryRow("SELECT id, user_id, solver_id, captcha_type, sitekey, target_url, captcha_response FROM tasks WHERE id = ?", taskID).
		Scan(&task.ID, &task.UserID, &task.SolverID, &task.CaptchaType, &task.SiteKey, &task.TargetURL, &task.CaptchaResponse)
	if err != nil {
		return c.Status(404).SendString("Задача не найдена")
	}

	return c.Render("captcha", fiber.Map{
		"Title": "Решите капчу",
		"Task":  task,
		"User":  c.Locals("user").(*User),
	}, "layout")
}

// Обработка решения капчи
func handleCaptchaSolution(c *fiber.Ctx) error {
	idParam := c.Params("id")
	var taskID int64
	if _, err := fmt.Sscan(idParam, &taskID); err != nil {
		return c.Status(400).SendString("Неверный ID задачи")
	}

	var task CaptchaTask
	err := db.QueryRow("SELECT id, user_id, solver_id, captcha_type, sitekey, target_url, captcha_response FROM tasks WHERE id = ?", taskID).
		Scan(&task.ID, &task.UserID, &task.SolverID, &task.CaptchaType, &task.SiteKey, &task.TargetURL, &task.CaptchaResponse)
	if err != nil {
		return c.Status(404).SendString("Задача не найдена")
	}

	var captchaResponse string
	if task.CaptchaType == "recaptcha" {
		captchaResponse = c.FormValue("g-recaptcha-response")
	} else {
		captchaResponse = c.FormValue("h-captcha-response")
	}

	if captchaResponse == "" {
		return c.Status(400).SendString("Необходимо решить капчу")
	}

	// Получаем пользователя, решающего задачу (worker)
	currentUser := c.Locals("user").(*User)
	_, err = db.Exec("UPDATE tasks SET captcha_response = ?, solver_id = ? WHERE id = ?", captchaResponse, currentUser.ID, taskID)
	if err != nil {
		return c.Status(500).SendString("Ошибка обновления задачи")
	}
	task.CaptchaResponse = captchaResponse
	task.SolverID = currentUser.ID

	// Отправляем результат в очередь результатов RabbitMQ
	taskBytes, err := json.Marshal(task)
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		rabbitMQChannel.PublishWithContext(ctx,
			"",
			"captcha_results",
			false,
			false,
			amqp.Publishing{
				ContentType: "application/json",
				Body:        taskBytes,
			})
	}

	return c.SendString("Капча успешно решена!")
}

// API: Получение следующей задачи
func getNextTask(c *fiber.Ctx) error {
	var task CaptchaTask
	err := db.QueryRow("SELECT id, user_id, solver_id, captcha_type, sitekey, target_url, captcha_response FROM tasks WHERE (captcha_response IS NULL OR captcha_response = '') LIMIT 1").
		Scan(&task.ID, &task.UserID, &task.SolverID, &task.CaptchaType, &task.SiteKey, &task.TargetURL, &task.CaptchaResponse)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "Нет доступных задач"})
	}
	return c.JSON(task)
}

// API: Получение количества задач в очереди
func getQueueCount(c *fiber.Ctx) error {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM tasks WHERE captcha_response IS NULL OR captcha_response = ''").Scan(&count)
	if err != nil {
		count = 0
	}
	return c.JSON(fiber.Map{"count": count})
}

// consumeTasks читает сообщения из RabbitMQ и вставляет/обновляет задачи в БД
func consumeTasks() {
	msgs, err := rabbitMQChannel.Consume(
		queueName, // очередь
		"",        // consumer
		true,      // auto-ack
		false,     // exclusive
		false,     // no-local
		false,     // no-wait
		nil,       // args
	)
	if err != nil {
		log.Fatalf("Не удалось зарегистрировать потребителя: %v", err)
	}

	for msg := range msgs {
		var task CaptchaTask
		if err := json.Unmarshal(msg.Body, &task); err != nil {
			log.Println("Ошибка декодирования сообщения:", err)
			continue
		}
		// Вставляем или обновляем задачу в БД
		_, err := db.Exec(`INSERT OR REPLACE INTO tasks 
			(id, user_id, solver_id, captcha_type, sitekey, target_url, captcha_response) 
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			task.ID, task.UserID, task.SolverID, task.CaptchaType, task.SiteKey, task.TargetURL, task.CaptchaResponse)
		if err != nil {
			log.Println("Ошибка вставки задачи в БД:", err)
		}
		log.Printf("Задача получена из RabbitMQ: %+v\n", task)
	}
}

// Handle WebSocket connections
func handleWebSocket(c *websocket.Conn) {
	defer c.Close()

	// Read authentication message
	_, msg, err := c.ReadMessage()
	if err != nil {
		log.Println("auth read error:", err)
		return
	}

	var auth AuthRequest
	if err := json.Unmarshal(msg, &auth); err != nil {
		log.Println("❌ Некорректный JSON в WebSocket")
		return
	}

	// Simple auth check - matching the other server
	if auth.ApiKey != "Hacker228" {
		log.Println("❌ Неверный API ключ через WebSocket:", auth.ApiKey)
		return
	}

	log.Println("✅ WebSocket авторизован")

	// Wait a bit before sending the task
	time.Sleep(1 * time.Second)

	// Send a sample task
	task := Task{
		Type:    "recaptcha",
		SiteKey: "6LeEnRsTAAAAAPHVIS06iy22BKCxrBsvyC7IrTVi",
		URL:     "https://deathbycaptcha.com/register",
	}

	// Send the task to the client
	if err := c.WriteJSON(task); err != nil {
		log.Println("Error sending task over WebSocket:", err)
	}
}

// Simple auth endpoint for electron app
func handleSimpleAuth(c *fiber.Ctx) error {
	var req AuthRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"status": "bad_request"})
	}

	log.Println("🔐 Авторизация от:", req.ApiKey)

	if req.ApiKey == "Hacker228" {
		return c.JSON(fiber.Map{
			"status":  "ok",
			"balance": 123.45,
		})
	}

	return c.Status(401).JSON(fiber.Map{"status": "unauthorized"})
}
