package data

import (
	"captcha-solver/internal/config"
	"captcha-solver/internal/models"
	"captcha-solver/internal/utils"

	"github.com/gofiber/fiber/v2"
)

// Обновление (регенерация) API ключа
func RegenerateAPIKey(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	apiKey, err := utils.GenerateAPIKey()
	if err != nil {
		return c.Status(500).SendString("Ошибка генерации API ключа")
	}

	_, err = config.DB.Exec("UPDATE users SET api_key = ? WHERE id = ?", apiKey, user.ID)
	if err != nil {
		return c.Status(500).SendString("Ошибка обновления API ключа")
	}
	user.APIKey = apiKey
	return c.JSON(fiber.Map{
		"api_key": apiKey,
	})
}
