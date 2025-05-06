package main

import (
	"captcha-solver/internal/config"
	"captcha-solver/internal/data"
	"captcha-solver/internal/db"
	"captcha-solver/internal/rabbitmq"
	"captcha-solver/internal/routes"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/template/html/v2"
	"log"
)

func main() {
	// Connect to RabbitMQ
	rabbitmq.RabbitMQConnect()

	defer rabbitmq.RabbitMQConn.Close()
	defer rabbitmq.RabbitMQChannel.Close()

	// Open or create the SQLite database
	db.DB_Connect()
	defer config.DB.Close()

	// Start RabbitMQ consumer in a goroutine
	go rabbitmq.ConsumeTasks()

	// Initialize HTML template engine (templates in folder views)
	engine := html.New("./views", ".html")
	app := fiber.New(fiber.Config{
		Views: engine,
	})

	routes.SetupRoutes(app)

	data.RootRedirect(app)

	log.Println("Server running on http://localhost:3058")
	log.Fatal(app.Listen(":8080"))
}
