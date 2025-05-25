package handlers

import (
	"captcha-solver/internal/config"
	"captcha-solver/internal/middleware"
	"captcha-solver/internal/models"
	"captcha-solver/internal/rabbitmq"
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Handle WebSocket connections
func HandleWebSocket(c *websocket.Conn) {
	defer c.Close()

	// Read authentication message
	_, msg, err := c.ReadMessage()
	if err != nil {
		log.Println("auth read error:", err)
		return
	}

	// Log the raw authentication message
	log.Println("📥 WebSocket AUTH RAW:", string(msg))

	var auth middleware.AuthRequest
	if err := json.Unmarshal(msg, &auth); err != nil {
		log.Println("❌ Некорректный JSON в WebSocket")
		if err := c.WriteJSON(map[string]string{
			"status":  "error",
			"message": "Invalid JSON format",
		}); err != nil {
			log.Println("Error sending error message:", err)
		}
		return
	}
	log.Printf("📥 WebSocket AUTH DECODED: %+v", auth)

	// Authenticate worker by API key
	var user models.User
	err = config.DB.QueryRow("SELECT id, username, role, balance FROM users WHERE api_key = ?", auth.ApiKey).
		Scan(&user.ID, &user.Username, &user.Role, &user.Balance)

	if err != nil {
		if err == sql.ErrNoRows {
			log.Println("❌ Неверный API ключ через WebSocket:", auth.ApiKey)
			if err := c.WriteJSON(map[string]string{
				"status":  "error",
				"message": "Invalid API key",
			}); err != nil {
				log.Println("Error sending error message:", err)
			}
		} else {
			log.Println("❌ Ошибка проверки API ключа:", err)
			if err := c.WriteJSON(map[string]string{
				"status":  "error",
				"message": "Server error during authentication",
			}); err != nil {
				log.Println("Error sending error message:", err)
			}
		}
		return
	}

	// Check if user has appropriate role for WebSocket connection
	if user.Role != "worker" && user.Role != "admin" && user.Role != "client" {
		log.Printf("❌ Пользователь %s имеет недопустимую роль: %s", user.Username, user.Role)
		if err := c.WriteJSON(map[string]string{
			"status":  "error",
			"message": "Only workers, clients and admins can connect via WebSocket",
		}); err != nil {
			log.Println("Error sending error message:", err)
		}
		return
	}

	log.Printf("✅ WebSocket авторизован для пользователя %s (ID: %d, роль: %s)\n", user.Username, user.ID, user.Role)

	// Send authentication success message
	authSuccessMsg := map[string]interface{}{
		"status":   "ok",
		"balance":  user.Balance,
		"username": user.Username,
		"role":     user.Role,
	}
	if err := c.WriteJSON(authSuccessMsg); err != nil {
		log.Println("Error sending auth success message:", err)
		return
	}

	// Main message loop - process incoming messages
	for {
		_, msgBytes, err := c.ReadMessage()
		if err != nil {
			log.Println("WebSocket read error:", err)
			break
		}

		// Log the raw message
		log.Println("📥 WebSocket MSG RAW:", string(msgBytes))

		// Try to parse as a command message first
		var commandMsg struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(msgBytes, &commandMsg); err != nil {
			log.Println("❌ Invalid message JSON:", err)
			continue
		}

		// Handle different message types
		switch commandMsg.Command {
		case "get_task":
			if user.Role != "worker" && user.Role != "admin" {
				c.WriteJSON(map[string]string{"status": "error", "message": "Only workers and admins can get tasks"})
				continue
			}
			fetchAndSendTask(c, user)

		case "submit_solution":
			if user.Role != "worker" && user.Role != "admin" {
				c.WriteJSON(map[string]string{"status": "error", "message": "Only workers and admins can submit solutions"})
				continue
			}
			var solutionData models.Task
			if err := json.Unmarshal(msgBytes, &solutionData); err != nil {
				log.Println("❌ Invalid solution JSON:", err)
				continue
			}

			// Process the solution
			if solutionData.TaskId > 0 && solutionData.Solution != "" {
				log.Printf("✅ Received solution for task #%d from worker %s\n", solutionData.TaskId, user.Username)
				log.Printf("📝 Solution content: %s", solutionData.Solution)

				// Update the task with the solution
				_, err = config.DB.Exec(
					"UPDATE tasks SET captcha_response = ? WHERE id = ? AND solver_id = ?",
					solutionData.Solution, solutionData.TaskId, user.ID)

				if err != nil {
					log.Println("Error saving solution:", err)
					errorMsg := map[string]string{"status": "error", "message": "Failed to save solution"}
					if err := c.WriteJSON(errorMsg); err != nil {
						log.Println("Error sending error message:", err)
					}
				} else {
					// Confirm solution received
					confirmMsg := map[string]string{"status": "solution_saved"}
					if err := c.WriteJSON(confirmMsg); err != nil {
						log.Println("Error sending confirmation:", err)
					}
				}
			}

		case "create_task":
			if user.Role != "client" && user.Role != "admin" {
				c.WriteJSON(map[string]string{"status": "error", "message": "Only clients and admins can create tasks"})
				continue
			}
			// Client is creating a new task
			var taskData struct {
				SiteKey     string `json:"sitekey"`
				TargetURL   string `json:"target_url"`
				CaptchaType string `json:"captcha_type"`
			}
			if err := json.Unmarshal(msgBytes, &taskData); err != nil {
				log.Println("❌ Invalid task JSON:", err)
				continue
			}

			if taskData.SiteKey == "" || taskData.TargetURL == "" {
				errorMsg := map[string]string{"status": "error", "message": "Sitekey and target URL are required"}
				if err := c.WriteJSON(errorMsg); err != nil {
					log.Println("Error sending error message:", err)
				}
				continue
			}

			if taskData.CaptchaType == "" {
				taskData.CaptchaType = "hcaptcha" // Default type
			}

			// Insert task with proper timestamp
			res, err := config.DB.Exec("INSERT INTO tasks (user_id, captcha_type, sitekey, target_url, created_at) VALUES (?, ?, ?, ?, ?)",
				user.ID, taskData.CaptchaType, taskData.SiteKey, taskData.TargetURL, time.Now().Format(time.RFC3339))
			if err != nil {
				log.Println("Error creating task:", err)
				errorMsg := map[string]string{"status": "error", "message": "Failed to create task"}
				if err := c.WriteJSON(errorMsg); err != nil {
					log.Println("Error sending error message:", err)
				}
				continue
			}

			taskID, _ := res.LastInsertId()
			task := &models.CaptchaTask{
				ID:          taskID,
				UserID:      user.ID,
				CaptchaType: taskData.CaptchaType,
				SiteKey:     taskData.SiteKey,
				TargetURL:   taskData.TargetURL,
			}

			// Send to RabbitMQ
			taskBytes, err := json.Marshal(task)
			if err != nil {
				log.Println("Error marshaling task:", err)
				errorMsg := map[string]string{"status": "error", "message": "Failed to process task"}
				if err := c.WriteJSON(errorMsg); err != nil {
					log.Println("Error sending error message:", err)
				}
				continue
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
				log.Println("Error publishing to RabbitMQ:", err)
				errorMsg := map[string]string{"status": "error", "message": "Failed to queue task"}
				if err := c.WriteJSON(errorMsg); err != nil {
					log.Println("Error sending error message:", err)
				}
				continue
			}

			successMsg := map[string]interface{}{
				"status": "success",
				"task":   task,
			}
			if err := c.WriteJSON(successMsg); err != nil {
				log.Println("Error sending success message:", err)
			}

		case "get_tasks":
			// Client is requesting all tasks
			rows, err := config.DB.Query("SELECT id, user_id, solver_id, captcha_type, sitekey, target_url, captcha_response FROM tasks WHERE user_id = ?", user.ID)
			if err != nil {
				log.Println("Error fetching tasks:", err)
				errorMsg := map[string]string{"status": "error", "message": "Failed to retrieve tasks"}
				if err := c.WriteJSON(errorMsg); err != nil {
					log.Println("Error sending error message:", err)
				}
				continue
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

			successMsg := map[string]interface{}{
				"status": "success",
				"tasks":  tasksList,
			}
			if err := c.WriteJSON(successMsg); err != nil {
				log.Println("Error sending success message:", err)
			}

		case "get_queue_count":
			// Client is requesting queue count
			var count int
			err := config.DB.QueryRow("SELECT COUNT(*) FROM tasks WHERE captcha_response IS NULL OR captcha_response = ''").Scan(&count)
			if err != nil {
				log.Println("Error fetching queue count:", err)
				errorMsg := map[string]string{"status": "error", "message": "Failed to retrieve queue count"}
				if err := c.WriteJSON(errorMsg); err != nil {
					log.Println("Error sending error message:", err)
				}
				continue
			}

			successMsg := map[string]interface{}{
				"status": "success",
				"count":  count,
			}
			if err := c.WriteJSON(successMsg); err != nil {
				log.Println("Error sending success message:", err)
			}

		default:
			// Unknown command
			log.Printf("⚠️ Unknown command: %s", commandMsg.Command)
			errorMsg := map[string]string{"status": "error", "message": "Unknown command"}
			if err := c.WriteJSON(errorMsg); err != nil {
				log.Println("Error sending error message:", err)
			}
		}
	}
}

// Helper function to fetch and send a task
func fetchAndSendTask(c *websocket.Conn, user models.User) {
	var taskID int64
	var siteKey, targetURL, captchaType string

	// Спочатку перевіряємо, чи є вже призначені завдання для цього робітника
	err := config.DB.QueryRow(`
		SELECT id, captcha_type, sitekey, target_url 
		FROM tasks 
		WHERE solver_id IS NULL AND (captcha_response IS NULL OR captcha_response = '')
		ORDER BY created_at ASC
		LIMIT 1
	`, user.ID).Scan(&taskID, &captchaType, &siteKey, &targetURL)

	if err == nil {
		// Знайдено призначене завдання
		task := models.Task{
			Type:    captchaType,
			SiteKey: siteKey,
			URL:     targetURL,
			TaskId:  taskID,
		}

		// Log the task being sent
		taskJson, _ := json.Marshal(task)
		log.Printf("📤 WebSocket SEND TASK (assigned): %s", string(taskJson))

		if err := c.WriteJSON(task); err != nil {
			log.Println("Error sending task over WebSocket:", err)
		} else {
			log.Printf("Task #%d sent to worker %s (ID: %d)\n", taskID, user.Username, user.ID)
		}
		return
	}

	// Якщо немає призначених завдань, шукаємо нове
	err = config.DB.QueryRow(`
		SELECT id, captcha_type, sitekey, target_url 
		FROM tasks 
		WHERE solver_id IS NULL AND captcha_response IS NULL
		ORDER BY created_at ASC
		LIMIT 1
	`).Scan(&taskID, &captchaType, &siteKey, &targetURL)

	if err != nil {
		if err != sql.ErrNoRows {
			log.Println("Error fetching task:", err)
			errorMsg := map[string]string{"status": "error", "message": "Database error"}
			if err := c.WriteJSON(errorMsg); err != nil {
				log.Println("Error sending error message:", err)
			}
		} else {
			// No tasks available
			noTaskMsg := map[string]string{"status": "no_tasks"}
			if err := c.WriteJSON(noTaskMsg); err != nil {
				log.Println("Error sending no-task message:", err)
			}
		}
		return
	}

	// Assign the task to this worker
	_, err = config.DB.Exec("UPDATE tasks SET solver_id = ? WHERE id = ?", user.ID, taskID)
	if err != nil {
		log.Println("Error assigning task to worker:", err)
		errorMsg := map[string]string{"status": "error", "message": "Failed to assign task"}
		if err := c.WriteJSON(errorMsg); err != nil {
			log.Println("Error sending error message:", err)
		}
		return
	}

	// Send the task
	task := models.Task{
		Type:    captchaType,
		SiteKey: siteKey,
		URL:     targetURL,
		TaskId:  taskID,
	}

	// Log the task being sent
	taskJson, _ := json.Marshal(task)
	log.Printf("📤 WebSocket SEND TASK (new): %s", string(taskJson))

	if err := c.WriteJSON(task); err != nil {
		log.Println("Error sending task over WebSocket:", err)
		// If failed to send, unassign the task
		_, _ = config.DB.Exec("UPDATE tasks SET solver_id = NULL WHERE id = ?", taskID)
	} else {
		log.Printf("Task #%d assigned to worker %s (ID: %d)\n", taskID, user.Username, user.ID)
	}
}

// Simple auth endpoint for electron app
func HandleSimpleAuth(c *fiber.Ctx) error {
	var req middleware.AuthRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{
			"status":  "error",
			"message": "Invalid request format",
		})
	}

	// Log the authentication request
	log.Printf("📥 API AUTH REQUEST: %+v", req)

	// Check API key in database
	var user models.User
	err := config.DB.QueryRow("SELECT id, username, role, balance FROM users WHERE api_key = ?", req.ApiKey).
		Scan(&user.ID, &user.Username, &user.Role, &user.Balance)

	if err != nil {
		if err == sql.ErrNoRows {
			log.Println("🔐 Неудачная авторизация с API ключом:", req.ApiKey)
			return c.Status(401).JSON(fiber.Map{
				"status":  "error",
				"message": "Invalid API key",
			})
		}
		log.Println("DB error during API auth:", err)
		return c.Status(500).JSON(fiber.Map{
			"status":  "error",
			"message": "Server error during authentication",
		})
	}

	// Check if user has appropriate role
	if user.Role != "worker" && user.Role != "client" && user.Role != "admin" {
		log.Printf("🔐 Пользователь %s имеет недопустимую роль: %s", user.Username, user.Role)
		return c.Status(403).JSON(fiber.Map{
			"status":  "error",
			"message": "User role not allowed",
		})
	}

	response := fiber.Map{
		"status":   "ok",
		"balance":  user.Balance,
		"username": user.Username,
		"role":     user.Role,
	}

	// Log the response
	log.Printf("📤 API AUTH RESPONSE: %+v", response)
	log.Printf("🔐 Успешная авторизация от: %s (ID: %d, роль: %s)", user.Username, user.ID, user.Role)

	return c.JSON(response)
}
