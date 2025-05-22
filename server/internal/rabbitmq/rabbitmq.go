package rabbitmq

import (
	"captcha-solver/internal/config"
	"captcha-solver/internal/models"
	"encoding/json"
	"log"

	amqp "github.com/rabbitmq/amqp091-go"
)

var (
	err             error
	RabbitMQConn    *amqp.Connection
	RabbitMQChannel *amqp.Channel
)

func RabbitMQConnect() {
	RabbitMQConn, err = amqp.Dial("amqp://guest:guest@localhost:5672/")
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	RabbitMQChannel, err = RabbitMQConn.Channel()
	if err != nil {
		log.Fatalf("Failed to open RabbitMQ channel: %v", err)
	}
	_, err = RabbitMQChannel.QueueDeclare(
		config.QueueName, // queue name
		true,             // durable
		false,            // delete when unused
		false,            // exclusive
		false,            // no-wait
		nil,              // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare queue: %v", err)
	}
}

// consumeTasks читает сообщения из RabbitMQ и вставляет/обновляет задачи в БД
func ConsumeTasks() {
	msgs, err := RabbitMQChannel.Consume(
		config.QueueName, // очередь
		"",               // consumer
		true,             // auto-ack
		false,            // exclusive
		false,            // no-local
		false,            // no-wait
		nil,              // args
	)
	if err != nil {
		log.Fatalf("Не удалось зарегистрировать потребителя: %v", err)
	}

	for msg := range msgs {
		var task models.CaptchaTask
		if err := json.Unmarshal(msg.Body, &task); err != nil {
			log.Println("Ошибка декодирования сообщения:", err)
			continue
		}
		// Вставляем или обновляем задачу в БД
		_, err := config.DB.Exec(`INSERT OR REPLACE INTO tasks 
			(id, user_id, solver_id, captcha_type, sitekey, target_url, captcha_response, created_at) 
			VALUES (?, ?, ?, ?, ?, ?, ?, COALESCE(?, datetime('now')) )`,
			task.ID, task.UserID, task.SolverID, task.CaptchaType, task.SiteKey, task.TargetURL, task.CaptchaResponse, task.CreatedAt)
		if err != nil {
			log.Println("Ошибка вставки задачи в БД:", err)
		}
		log.Printf("Задача получена из RabbitMQ: %+v\n", task)
	}
}
