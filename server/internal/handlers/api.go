package handlers

import (
	"captcha-solver/internal/config"
	"captcha-solver/internal/models"
	"database/sql"
	"log"

	"github.com/gofiber/fiber/v2"
)

// GetNextTaskAPI отримує наступне завдання для робітника
func GetNextTaskAPI(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	if user.Role != "worker" && user.Role != "admin" {
		return c.Status(403).JSON(fiber.Map{
			"status":  "error",
			"message": "Only workers and admins can get tasks",
		})
	}

	var taskID int64
	var siteKey, targetURL, captchaType string

	err := config.DB.QueryRow(`
		SELECT id, captcha_type, sitekey, target_url 
		FROM tasks 
		WHERE solver_id IS NULL AND (captcha_response IS NULL OR captcha_response = '')
		ORDER BY created_at ASC
		LIMIT 1
	`).Scan(&taskID, &captchaType, &siteKey, &targetURL)

	if err != nil {
		if err == sql.ErrNoRows {
			return c.Status(404).JSON(fiber.Map{
				"status":  "no_tasks",
				"message": "No tasks available",
			})
		}
		log.Println("Error fetching task:", err)
		return c.Status(500).JSON(fiber.Map{
			"status":  "error",
			"message": "Database error",
		})
	}

	// Assign the task to this worker
	_, err = config.DB.Exec("UPDATE tasks SET solver_id = ? WHERE id = ?", user.ID, taskID)
	if err != nil {
		log.Println("Error assigning task to worker:", err)
		return c.Status(500).JSON(fiber.Map{
			"status":  "error",
			"message": "Failed to assign task",
		})
	}

	task := models.Task{
		Type:    captchaType,
		SiteKey: siteKey,
		URL:     targetURL,
		TaskId:  taskID,
	}

	return c.JSON(fiber.Map{
		"status": "success",
		"task":   task,
	})
}

// SubmitSolutionAPI відправляє рішення капчі
func SubmitSolutionAPI(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	if user.Role != "worker" && user.Role != "admin" {
		return c.Status(403).JSON(fiber.Map{
			"status":  "error",
			"message": "Only workers and admins can submit solutions",
		})
	}

	var solution struct {
		TaskID   int64  `json:"task_id"`
		Solution string `json:"solution"`
	}

	if err := c.BodyParser(&solution); err != nil {
		return c.Status(400).JSON(fiber.Map{
			"status":  "error",
			"message": "Invalid request format",
		})
	}

	if solution.TaskID <= 0 || solution.Solution == "" {
		return c.Status(400).JSON(fiber.Map{
			"status":  "error",
			"message": "Task ID and solution are required",
		})
	}

	// Update the task with the solution
	_, err := config.DB.Exec(
		"UPDATE tasks SET captcha_response = ? WHERE id = ? AND solver_id = ?",
		solution.Solution, solution.TaskID, user.ID)

	if err != nil {
		log.Println("Error saving solution:", err)
		return c.Status(500).JSON(fiber.Map{
			"status":  "error",
			"message": "Failed to save solution",
		})
	}

	return c.JSON(fiber.Map{
		"status": "solution_saved",
	})
}

// GetQueueCountAPI отримує кількість завдань в черзі
func GetQueueCountAPI(c *fiber.Ctx) error {
	var count int
	err := config.DB.QueryRow("SELECT COUNT(*) FROM tasks WHERE solver_id IS NULL AND (captcha_response IS NULL OR captcha_response = '')").Scan(&count)
	if err != nil {
		log.Println("Error fetching queue count:", err)
		return c.Status(500).JSON(fiber.Map{
			"status":  "error",
			"message": "Failed to retrieve queue count",
		})
	}

	return c.JSON(fiber.Map{
		"status": "success",
		"count":  count,
	})
}
