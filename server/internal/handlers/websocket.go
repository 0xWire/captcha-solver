package handlers

import (
	"captcha-solver/internal/config"
	"captcha-solver/internal/middleware"
	"captcha-solver/internal/models"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
	"log"
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
		return
	}
	log.Printf("📥 WebSocket AUTH DECODED: %+v", auth)

	// Authenticate worker by API key
	var user models.User
	err = config.DB.QueryRow("SELECT id, username, role FROM users WHERE api_key = ? AND (role = 'worker' OR role = 'admin')", auth.ApiKey).
		Scan(&user.ID, &user.Username, &user.Role)

	if err != nil {
		if err == sql.ErrNoRows {
			log.Println("❌ Неверный API ключ через WebSocket:", auth.ApiKey)
		} else {
			log.Println("❌ Ошибка проверки API ключа:", err)
		}
		return
	}

	log.Printf("✅ WebSocket авторизован для пользователя %s (ID: %d, роль: %s)\n", user.Username, user.ID, user.Role)

	// Send authentication success message
	authSuccessMsg := map[string]string{"status": "ok", "balance": fmt.Sprintf("%.2f", user.Balance), "username": user.Username}
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
			// Client is requesting a task
			fetchAndSendTask(c, user)

		case "submit_solution":
			// Client is submitting a solution
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

	err := config.DB.QueryRow(`
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
	log.Println("📤 WebSocket SEND TASK:", string(taskJson))

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
		return c.Status(400).JSON(fiber.Map{"status": "bad_request"})
	}

	// Log the authentication request
	log.Printf("📥 API AUTH REQUEST: %+v", req)

	// Check API key in database
	var user models.User
	err := config.DB.QueryRow("SELECT id, username, balance FROM users WHERE api_key = ?", req.ApiKey).
		Scan(&user.ID, &user.Username, &user.Balance)

	if err != nil {
		if err == sql.ErrNoRows {
			log.Println("🔐 Неудачная авторизация с API ключом:", req.ApiKey)
			return c.Status(401).JSON(fiber.Map{"status": "unauthorized"})
		}
		log.Println("DB error during API auth:", err)
		return c.Status(500).JSON(fiber.Map{"status": "server_error"})
	}

	response := fiber.Map{
		"status":   "ok",
		"balance":  user.Balance,
		"username": user.Username,
	}

	// Log the response
	log.Printf("📤 API AUTH RESPONSE: %+v", response)
	log.Printf("🔐 Успешная авторизация от: %s (ID: %d)", user.Username, user.ID)

	return c.JSON(response)
}
