package handlers

import (
	"captcha-solver/internal/config"
	"captcha-solver/internal/models"
	"fmt"

	"github.com/gofiber/fiber/v2"
)

// Личный кабинет клиента
func ShowClientDashboard(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	rows, err := config.DB.Query("SELECT id, user_id, solver_id, captcha_type, sitekey, target_url, captcha_response FROM tasks WHERE user_id = ?", user.ID)
	if err != nil {
		return c.Status(500).SendString("Ошибка получения задач")
	}
	defer rows.Close()

	var clientTasks []*models.CaptchaTask
	for rows.Next() {
		var task models.CaptchaTask
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

// Получение всех задач для клиента
func GetTasks(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	rows, err := config.DB.Query("SELECT id, user_id, solver_id, captcha_type, sitekey, target_url, captcha_response FROM tasks WHERE user_id = ?", user.ID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to retrieve tasks"})
	}
	defer rows.Close()

	var tasksList []*models.CaptchaTask
	for rows.Next() {
		var task models.CaptchaTask
		if err := rows.Scan(&task.ID, &task.UserID, &task.SolverID, &task.CaptchaType, &task.SiteKey, &task.TargetURL, &task.CaptchaResponse); err != nil {
			continue
		}
		tasksList = append(tasksList, &task)
	}
	return c.JSON(tasksList)
}

// Получение конкретной задачи для клиента
func GetTask(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	idParam := c.Params("id")
	var taskID int64
	if _, err := fmt.Sscan(idParam, &taskID); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid task ID"})
	}

	var task models.CaptchaTask
	err := config.DB.QueryRow(`
		SELECT id, user_id, solver_id, captcha_type, sitekey, target_url, captcha_response,
		       datetime(created_at, 'localtime') as created_at
		FROM tasks 
		WHERE id = ? AND user_id = ?
	`, taskID, user.ID).Scan(
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
		return c.Status(404).JSON(fiber.Map{"error": "Task not found"})
	}

	return c.JSON(task)
}

// SubmitCaptchaSolution handles solutions submitted by workers via WebSocket or API
func SubmitCaptchaSolution(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	// Only workers and admins can submit solutions
	if user.Role != "worker" && user.Role != "admin" {
		return c.Status(403).JSON(fiber.Map{"error": "Unauthorized"})
	}

	// Parse request
	var solution struct {
		TaskID   int64  `json:"task_id"`
		Solution string `json:"solution"`
	}

	if err := c.BodyParser(&solution); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request format"})
	}

	// Verify task exists and belongs to this worker
	var clientID int64
	err := config.DB.QueryRow("SELECT user_id FROM tasks WHERE id = ? AND solver_id = ?",
		solution.TaskID, user.ID).Scan(&clientID)

	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "Task not found or not assigned to you"})
	}

	// Update the task with the solution
	_, err = config.DB.Exec(
		"UPDATE tasks SET captcha_response = ? WHERE id = ?",
		solution.Solution, solution.TaskID)

	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to save solution"})
	}

	// TODO: update client balance here

	return c.JSON(fiber.Map{"status": "success"})
}
