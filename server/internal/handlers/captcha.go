package handlers

import (
	"captcha-solver/internal/config"
	"captcha-solver/internal/models"
	"captcha-solver/internal/rabbitmq"
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	amqp "github.com/rabbitmq/amqp091-go"
)

// SubmitCaptcha обробляє відправку нової капчі
func SubmitCaptcha(c *fiber.Ctx) error {
	log.Printf("📥 Отримано запит на відправку капчі: %s", string(c.Body()))

	var taskData struct {
		SiteKey     string `json:"sitekey"`
		TargetURL   string `json:"target_url"`
		CaptchaType string `json:"captcha_type"`
	}

	if err := c.BodyParser(&taskData); err != nil {
		log.Printf("❌ Помилка парсингу запиту: %v", err)
		return c.Status(400).JSON(fiber.Map{
			"status":  "error",
			"message": "Invalid request format",
		})
	}

	log.Printf("📝 Дані запиту: %+v", taskData)

	// Отримуємо користувача з контексту (був доданий middleware.APIKeyMiddleware)
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		log.Printf("❌ Користувач не знайдений в контексті")
		return c.Status(401).JSON(fiber.Map{
			"status":  "error",
			"message": "User not found in context",
		})
	}

	log.Printf("✅ Користувач авторизований: %s (ID: %d, роль: %s)", user.Username, user.ID, user.Role)

	// Перевірка ролі
	if user.Role != "client" && user.Role != "admin" {
		log.Printf("❌ Неправильна роль користувача: %s", user.Role)
		return c.Status(403).JSON(fiber.Map{
			"status":  "error",
			"message": "Only clients and admins can submit captchas",
		})
	}

	if taskData.SiteKey == "" || taskData.TargetURL == "" {
		log.Printf("❌ Відсутні обов'язкові поля: sitekey=%s, target_url=%s", taskData.SiteKey, taskData.TargetURL)
		return c.Status(400).JSON(fiber.Map{
			"status":  "error",
			"message": "Sitekey and target URL are required",
		})
	}

	if taskData.CaptchaType == "" {
		taskData.CaptchaType = "hcaptcha"
	}

	// Створення завдання
	res, err := config.DB.Exec("INSERT INTO tasks (user_id, captcha_type, sitekey, target_url) VALUES (?, ?, ?, ?)",
		user.ID, taskData.CaptchaType, taskData.SiteKey, taskData.TargetURL)
	if err != nil {
		log.Printf("❌ Помилка створення завдання: %v", err)
		return c.Status(500).JSON(fiber.Map{
			"status":  "error",
			"message": "Failed to create task",
		})
	}

	taskID, _ := res.LastInsertId()
	task := &models.CaptchaTask{
		ID:          taskID,
		UserID:      user.ID,
		CaptchaType: taskData.CaptchaType,
		SiteKey:     taskData.SiteKey,
		TargetURL:   taskData.TargetURL,
	}

	log.Printf("✅ Створено завдання #%d для користувача %s", taskID, user.Username)

	// Відправка в RabbitMQ
	taskBytes, err := json.Marshal(task)
	if err != nil {
		log.Printf("❌ Помилка серіалізації завдання: %v", err)
		return c.Status(500).JSON(fiber.Map{
			"status":  "error",
			"message": "Failed to process task",
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = rabbitmq.RabbitMQChannel.PublishWithContext(ctx,
		"",               // exchange
		config.QueueName, // routing key
		false,            // mandatory
		false,            // immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        taskBytes,
		})
	if err != nil {
		log.Printf("❌ Помилка відправки в RabbitMQ: %v", err)
		return c.Status(500).JSON(fiber.Map{
			"status":  "error",
			"message": "Failed to queue task",
		})
	}

	log.Printf("✅ Завдання #%d успішно додано до черги", taskID)

	return c.JSON(fiber.Map{
		"status": "success",
		"task":   task,
	})
}

// GetCaptchaResult отримує результат капчі за ID
func GetCaptchaResult(c *fiber.Ctx) error {
	taskID := c.Params("id")
	var task models.CaptchaTask

	err := config.DB.QueryRow(`
		SELECT id, user_id, solver_id, captcha_type, sitekey, target_url, captcha_response, 
		       datetime(created_at, 'localtime') as created_at 
		FROM tasks 
		WHERE id = ?
	`, taskID).Scan(
		&task.ID,
		&task.UserID,
		&task.SolverID,
		&task.CaptchaType,
		&task.SiteKey,
		&task.TargetURL,
		&task.CaptchaResponse,
		&task.CreatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return c.Status(404).JSON(fiber.Map{
				"status":  "error",
				"message": "Task not found",
			})
		}
		return c.Status(500).JSON(fiber.Map{
			"status":  "error",
			"message": "Database error",
		})
	}

	return c.JSON(fiber.Map{
		"status": "success",
		"task":   task,
	})
}

// SubmitSolution обробляє відправку розв'язку капчі
func SubmitSolution(c *fiber.Ctx) error {
	var solutionData struct {
		ApiKey   string `json:"api_key"`
		TaskID   int64  `json:"task_id"`
		Solution string `json:"solution"`
	}

	if err := c.BodyParser(&solutionData); err != nil {
		return c.Status(400).JSON(fiber.Map{
			"status":  "error",
			"message": "Invalid request format",
		})
	}

	// Перевірка API ключа
	var user models.User
	err := config.DB.QueryRow("SELECT id, username, role FROM users WHERE api_key = ?", solutionData.ApiKey).
		Scan(&user.ID, &user.Username, &user.Role)

	if err != nil {
		if err == sql.ErrNoRows {
			return c.Status(401).JSON(fiber.Map{
				"status":  "error",
				"message": "Invalid API key",
			})
		}
		return c.Status(500).JSON(fiber.Map{
			"status":  "error",
			"message": "Server error during authentication",
		})
	}

	// Перевірка ролі
	if user.Role != "worker" && user.Role != "admin" {
		return c.Status(403).JSON(fiber.Map{
			"status":  "error",
			"message": "Only workers and admins can submit solutions",
		})
	}

	if solutionData.TaskID <= 0 || solutionData.Solution == "" {
		return c.Status(400).JSON(fiber.Map{
			"status":  "error",
			"message": "Task ID and solution are required",
		})
	}

	// Оновлення завдання з розв'язком
	_, err = config.DB.Exec(
		"UPDATE tasks SET captcha_response = ?, solver_id = ? WHERE id = ?",
		solutionData.Solution, user.ID, solutionData.TaskID)

	if err != nil {
		log.Println("Error saving solution:", err)
		return c.Status(500).JSON(fiber.Map{
			"status":  "error",
			"message": "Failed to save solution",
		})
	}

	return c.JSON(fiber.Map{
		"status":  "success",
		"message": "Solution saved successfully",
	})
}
