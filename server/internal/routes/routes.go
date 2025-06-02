package routes

import (
	"captcha-solver/internal/data"
	"captcha-solver/internal/handlers"
	"captcha-solver/internal/middleware"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
)

func SetupRoutes(app *fiber.App) {
	// API routes - повинні бути першими, щоб уникнути конфлікту з сесійною аутентифікацією
	apiGroup := app.Group("/api")
	apiGroup.Post("/captcha/submit", middleware.APIKeyMiddleware, handlers.SubmitCaptcha) // Прийом капчі від клієнта
	apiGroup.Get("/captcha/result/:id", middleware.APIKeyMiddleware, handlers.GetCaptchaResult)
	apiGroup.Post("/captcha/solution", middleware.APIKeyMiddleware, handlers.SubmitSolution)

	// Public routes
	app.Get("/login", handlers.ShowLoginPage)
	app.Post("/login", handlers.HandleLogin)
	app.Get("/register", handlers.ShowRegisterPage)
	app.Post("/register", handlers.HandleRegister)
	app.Get("/logout", handlers.HandleLogout)

	// Add websocket route with auth check
	app.Get("/socket", websocket.New(handlers.HandleWebSocket))

	// Auth endpoint for worker client
	app.Post("/auth", handlers.HandleSimpleAuth)

	// Protected routes – requires session authentication
	authGroup := app.Group("/", middleware.AuthMiddleware)
	authGroup.Get("/result/:id", handlers.ShowResult)

	// Admin routes
	adminGroup := authGroup.Group("/admin", middleware.RoleMiddleware("admin"))
	adminGroup.Get("/", handlers.ShowAdminDashboard)
	adminGroup.Get("/users", handlers.ShowUsersAdmin)
	adminGroup.Post("/users", handlers.CreateUser)
	adminGroup.Delete("/users/:id", handlers.DeleteUser)
	adminGroup.Get("/tasks", handlers.ShowTaskList)

	// Worker routes (with prefix /worker)
	workerGroup := authGroup.Group("/worker", middleware.RoleMiddleware("admin", "worker"))
	workerGroup.Get("/solve-queue", handlers.ShowSolveQueue)
	workerGroup.Get("/captcha/:id", handlers.ShowCaptcha)
	workerGroup.Post("/solve/:id", handlers.HandleCaptchaSolution)
	workerGroup.Get("/tasks", handlers.ShowTaskList)

	// Client routes (with prefix /client)
	clientGroup := authGroup.Group("/client", middleware.RoleMiddleware("admin", "client"))
	clientGroup.Get("/", handlers.ShowClientDashboard)
	clientGroup.Get("/api-key/regenerate", data.RegenerateAPIKey)

	// Shared API endpoints
	authGroup.Get("/api/next-task", handlers.GetNextTask)
	authGroup.Get("/api/queue-count", handlers.GetQueueCount)
}
