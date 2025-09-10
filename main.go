package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	handler "github.com/Flack74/GoChat/handlers"
	wsserver "github.com/Flack74/GoChat/websockets"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/template/html/v2"
	"github.com/gofiber/websocket/v2"
)

func main() {
	// Create views engine
	viewsEngine := html.New("./views", ".html")

	// Start new fiber instance
	app := fiber.New(fiber.Config{
		Views: viewsEngine,
	})

	// Static route and directory
	app.Static("/static/", "./static")

	// Create handlers
	appHandler := handler.NewAppHandler()

	// Routes
	app.Get("/", appHandler.HandleGetIndex)

	// Create WebSocket server
	wsServer := wsserver.NewWebSocketServer()

	// WebSocket middleware
	app.Use("/ws", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			c.Locals("allowed", true)
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})

	// WebSocket route
	app.Get("/ws", websocket.New(func(c *websocket.Conn) {
		wsServer.HandleWebSocket(c)
	}))

	// Start message handling goroutine
	go wsServer.HandleMessage()

	// Graceful shutdown
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)
		<-sigint

		log.Println("Shutting down server...")
		wsServer.ShutDown()
		app.Shutdown()
	}()

	// Start server
	log.Println("Server starting on :3000")

	log.Fatal(app.Listen(":3000"))
}
