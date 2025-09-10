package wsserver

import (
	"bytes"
	"encoding/json"
	"html/template"
	"log"
	"sync"
	"time"

	"github.com/gofiber/websocket/v2"
)

type Message struct {
	Text      string    `json:"text"`
	Username  string    `json:"username,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

type WebSocketServer struct {
	clients   map[*websocket.Conn]bool
	broadcast chan *Message
	mutex     sync.RWMutex
	done      chan bool
}

func NewWebSocketServer() *WebSocketServer {
	return &WebSocketServer{
		clients:   make(map[*websocket.Conn]bool),
		broadcast: make(chan *Message, 256), // Buffered channel
		done:      make(chan bool),
	}
}

func (s *WebSocketServer) HandleWebSocket(c *websocket.Conn) {
	// Register clients
	s.mutex.Lock()
	s.clients[c] = true
	clientCount := len(s.clients)
	s.mutex.Unlock()

	log.Printf("Client connected. Total clients: %d", clientCount)

	// Set up ping/pong handler for connection health
	c.SetPongHandler(func(string) error {
		c.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// Cleanup on disconnect
	defer func() {
		s.mutex.Lock()
		delete(s.clients, c)
		clientCount := len(s.clients)
		s.mutex.Unlock()

		c.Close()
		log.Printf("Client disconnected. Total clients: %v", clientCount)
	}()

	// Set read deadline
	c.SetReadDeadline(time.Now().Add(60 * time.Second))

	// Message reading loop
	for {
		_, msgBytes, err := c.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}
		// Reset read deadline
		c.SetReadDeadline(time.Now().Add(60 * time.Second))

		// Parse message
		var message Message
		if err := json.Unmarshal(msgBytes, &message); err != nil {
			log.Printf("Error unmarshaling message: %v", err)
			continue
		}

		// Add timestamp
		message.Timestamp = time.Now()

		// validate message
		if len(message.Text) == 0 || len(message.Text) > 500 {
			continue
		}

		// Broadcast message
		select {
		case s.broadcast <- &message:
		default:
			log.Println("Broadcast channel full, dropping message")
		}
	}
}

func (s *WebSocketServer) HandleMessage() {
	ticker := time.NewTicker(54 * time.Second) // Ping interval
	defer ticker.Stop()

	for {
		select {
		case message := <-s.broadcast:
			s.broadcastMessage(message)

		case <-ticker.C:
			s.pingClients()

		case <-s.done:
			log.Println("Message handler shutting down")
			return
		}
	}
}

func (s *WebSocketServer) broadcastMessage(message *Message) {
	renderMessage := s.getMessageTemplate(message)

	s.mutex.RLock()
	clients := make([]*websocket.Conn, 0, len(s.clients))
	for client := range s.clients {
		clients = append(clients, client)
	}
	s.mutex.RUnlock()

	var wg sync.WaitGroup
	for _, client := range clients {
		wg.Add(1)
		go func(c *websocket.Conn) {
			defer wg.Done()

			if err := c.WriteMessage(websocket.TextMessage, renderMessage); err != nil {
				log.Printf("Write error: %v", err)

				// Remove failed client
				s.mutex.Lock()
				delete(s.clients, c)
				s.mutex.Unlock()
				c.Close()
			}
		}(client)
	}
	wg.Wait()
}

func (s *WebSocketServer) pingClients() {
	s.mutex.RLock()
	clients := make([]*websocket.Conn, 0, len(s.clients))
	for client := range s.clients {
		clients = append(clients, client)
	}
	s.mutex.RUnlock()

	for _, client := range clients {
		if err := client.WriteMessage(websocket.PingMessage, nil); err != nil {
			s.mutex.Lock()
			delete(s.clients, client)

			s.mutex.Unlock()
			client.Close()
		}
	}
}

func (s *WebSocketServer) getMessageTemplate(msg *Message) []byte {
	tmpl, err := template.ParseFiles("./views/message.html")
	if err != nil {
		log.Printf("Template parsing error: %v", err)
		return []byte("<div>Error rendering message</div>")
	}

	var renderedMessage bytes.Buffer
	if err := tmpl.Execute(&renderedMessage, msg); err != nil {
		log.Printf("Template execution error: %v", err)
		return []byte("<div>Error rendering message</div>")
	}
	return renderedMessage.Bytes()
}

func (s *WebSocketServer) ShutDown() {
	close(s.done)

	s.mutex.Lock()
	for client := range s.clients {
		client.Close()
	}
	s.clients = make(map[*websocket.Conn]bool)
	s.mutex.Unlock()

	log.Println("WebSocket server shut down")
}
