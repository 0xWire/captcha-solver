package middleware

import (
	"captcha-solver/internal/models"
	"log"
	"github.com/gofiber/fiber/v2"
)

// Middleware для проверки ролей
func RoleMiddleware(roles ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*models.User)
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
