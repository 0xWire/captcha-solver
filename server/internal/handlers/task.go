package handlers

import (
	"captcha-solver/internal/config"
	"captcha-solver/internal/models"
	"captcha-solver/internal/rabbitmq"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"
	"github.com/gofiber/fiber/v2"
	amqp "github.com/rabbitmq/amqp091-go"
)

// Создание задачи через API
func CreateTask(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
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
	res, err := config.DB.Exec("INSERT INTO tasks (user_id, captcha_type, sitekey, target_url, created_at) VALUES (?, ?, ?, ?, ?)",
		user.ID, payload.CaptchaType, payload.SiteKey, payload.TargetURL, time.Now())
	if err != nil {
		log.Printf("Database error when creating task: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to create task"})
	}

	taskID, _ := res.LastInsertId()
	task := &models.CaptchaTask{
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
		log.Printf("Error publishing to RabbitMQ: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "Failed to queue task"})
	}

	return c.JSON(task)
}

func ShowResult(c *fiber.Ctx) error {
	idParam := c.Params("id")
	var taskID int64
	if _, err := fmt.Sscan(idParam, &taskID); err != nil {
		return c.Status(400).SendString("Invalid task ID")
	}

	var task models.CaptchaTask
	err := config.DB.QueryRow("SELECT id, user_id, solver_id, captcha_type, sitekey, target_url, captcha_response FROM tasks WHERE id = ?", taskID).
		Scan(&task.ID, &task.UserID, &task.SolverID, &task.CaptchaType, &task.SiteKey, &task.TargetURL, &task.CaptchaResponse)
	if err != nil {
		return c.Status(404).SendString("Task not found")
	}

	// Render a result view (you can reuse an existing template or create a new one)
	return c.Render("result", fiber.Map{
		"Title": "Task Result",
		"Task":  task,
		"User":  c.Locals("user").(*models.User),
	}, "layout")
}

func ShowTaskList(c *fiber.Ctx) error {
	rows, err := config.DB.Query("SELECT id, user_id, solver_id, captcha_type, sitekey, target_url, captcha_response FROM tasks")
	if err != nil {
		return c.Status(500).SendString("Ошибка получения задач")
	}
	defer rows.Close()

	var tasks []*models.CaptchaTask
	for rows.Next() {
		var task models.CaptchaTask
		if err := rows.Scan(&task.ID, &task.UserID, &task.SolverID, &task.CaptchaType, &task.SiteKey, &task.TargetURL, &task.CaptchaResponse); err != nil {
			continue
		}
		tasks = append(tasks, &task)
	}
	return c.Render("index", fiber.Map{
		"User":  c.Locals("user").(*models.User),
		"Tasks": tasks,
	}, "layout")
}

// Очередь задач для решения (workers)
func ShowSolveQueue(c *fiber.Ctx) error {
	var count int
	err := config.DB.QueryRow("SELECT COUNT(*) FROM tasks WHERE captcha_response IS NULL OR captcha_response = ''").Scan(&count)
	if err != nil {
		count = 0
	}

	return c.Render("solve-queue", fiber.Map{
		"Title": "Решение капч",
		"Count": count,
		"User":  c.Locals("user").(*models.User),
	}, "layout")
}

// Страница решения капчи
func ShowCaptcha(c *fiber.Ctx) error {
	idParam := c.Params("id")
	var taskID int64
	if _, err := fmt.Sscan(idParam, &taskID); err != nil {
		return c.Status(400).SendString("Неверный ID задачи")
	}

	var task models.CaptchaTask
	err := config.DB.QueryRow("SELECT id, user_id, solver_id, captcha_type, sitekey, target_url, captcha_response FROM tasks WHERE id = ?", taskID).
		Scan(&task.ID, &task.UserID, &task.SolverID, &task.CaptchaType, &task.SiteKey, &task.TargetURL, &task.CaptchaResponse)
	if err != nil {
		return c.Status(404).SendString("Задача не найдена")
	}

	return c.Render("captcha", fiber.Map{
		"Title": "Решите капчу",
		"Task":  task,
		"User":  c.Locals("user").(*models.User),
	}, "layout")
}

// Обработка решения капчи
func HandleCaptchaSolution(c *fiber.Ctx) error {
	idParam := c.Params("id")
	var taskID int64
	if _, err := fmt.Sscan(idParam, &taskID); err != nil {
		return c.Status(400).SendString("Неверный ID задачи")
	}

	var task models.CaptchaTask
	err := config.DB.QueryRow("SELECT id, user_id, solver_id, captcha_type, sitekey, target_url, captcha_response FROM tasks WHERE id = ?", taskID).
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
	currentUser := c.Locals("user").(*models.User)
	_, err = config.DB.Exec("UPDATE tasks SET captcha_response = ?, solver_id = ? WHERE id = ?", captchaResponse, currentUser.ID, taskID)
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

		rabbitmq.RabbitMQChannel.PublishWithContext(ctx,
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
func GetNextTask(c *fiber.Ctx) error {
	var task models.CaptchaTask
	err := config.DB.QueryRow("SELECT id, user_id, solver_id, captcha_type, sitekey, target_url, captcha_response FROM tasks WHERE (captcha_response IS NULL OR captcha_response = '') LIMIT 1").
		Scan(&task.ID, &task.UserID, &task.SolverID, &task.CaptchaType, &task.SiteKey, &task.TargetURL, &task.CaptchaResponse)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "Нет доступных задач"})
	}
	return c.JSON(task)
}

// API: Получение количества задач в очереди
func GetQueueCount(c *fiber.Ctx) error {
	var count int
	err := config.DB.QueryRow("SELECT COUNT(*) FROM tasks WHERE captcha_response IS NULL OR captcha_response = ''").Scan(&count)
	if err != nil {
		count = 0
	}
	return c.JSON(fiber.Map{"count": count})
}
