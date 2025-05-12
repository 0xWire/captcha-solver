package main

import (
	"encoding/json"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
)

type AuthRequest struct {
	ApiKey string `json:"api_key"`
}

type Task struct {
	Type    string `json:"type"`
	SiteKey string `json:"sitekey"`
	URL     string `json:"url"`
}

func main() {
	app := fiber.New()

	app.Post("/auth", func(c *fiber.Ctx) error {
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
	})

	app.Get("/socket", websocket.New(func(c *websocket.Conn) {
		defer c.Close()

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

		if auth.ApiKey != "Hacker228" {
			log.Println("❌ Неверный API ключ через WebSocket:", auth.ApiKey)
			return
		}

		log.Println("✅ WebSocket авторизован")

		time.Sleep(1 * time.Second)

		task := Task{
			Type:    "recaptcha",
			SiteKey: "6LeEnRsTAAAAAPHVIS06iy22BKCxrBsvyC7IrTVi",
			URL:     "https://deathbycaptcha.com/register",
		}

		c.WriteJSON(task)
	}))

	log.Println("🔌 Server listened at :8080")
	log.Fatal(app.Listen(":8080"))
}