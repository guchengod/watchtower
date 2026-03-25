package websocket

import (
	"encoding/json"
	"sync"

	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

// Hub manages all WebSocket clients and broadcasts messages.
type Hub struct {
	// Clients is guarded by mutex for concurrent safety
	clients    map[*websocket.Conn]bool
	mu         sync.RWMutex
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	broadcast  chan []byte
	done       chan struct{}
}

// Message represents a broadcast message sent to all clients.
type Message struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// DefaultHub is the package-level singleton hub instance.
var DefaultHub = NewHub()

// NewHub creates a new Hub instance.
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*websocket.Conn]bool),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
		broadcast:  make(chan []byte, 256),
		done:       make(chan struct{}),
	}
}

// Run starts the hub's main event loop.
func (h *Hub) Run() {
	for {
		select {
		case <-h.done:
			return
		case conn := <-h.register:
			h.mu.Lock()
			h.clients[conn] = true
			h.mu.Unlock()
			log.Debug("WebSocket client connected")
		case conn := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[conn]; ok {
				delete(h.clients, conn)
				conn.Close()
			}
			h.mu.Unlock()
			log.Debug("WebSocket client disconnected")
		case message := <-h.broadcast:
			h.mu.RLock()
			for conn := range h.clients {
				if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
					log.WithError(err).Warn("Failed to write to WebSocket client")
					conn.Close()
					delete(h.clients, conn)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// AddClient registers a new WebSocket connection.
func (h *Hub) AddClient(conn *websocket.Conn) {
	h.register <- conn
}

// RemoveClient unregisters a WebSocket connection.
func (h *Hub) RemoveClient(conn *websocket.Conn) {
	h.unregister <- conn
}

// Broadcast sends a message to all connected clients.
func (h *Hub) Broadcast(msgType string, data interface{}) {
	msg := Message{Type: msgType, Data: data}
	payload, err := json.Marshal(msg)
	if err != nil {
		log.WithError(err).Warn("Failed to marshal WebSocket message")
		return
	}
	select {
	case h.broadcast <- payload:
	default:
		log.Warn("WebSocket broadcast channel full, dropping message")
	}
}

// BroadcastContainerUpdate sends a container update event to all clients.
func (h *Hub) BroadcastContainerUpdate(containerID, containerName, action string) {
	h.Broadcast("container_update", map[string]string{
		"container_id":   containerID,
		"container_name": containerName,
		"action":         action,
	})
}

// BroadcastScheduleUpdate sends a schedule change event to all clients.
func (h *Hub) BroadcastScheduleUpdate(scheduleSpec string) {
	h.Broadcast("schedule_update", map[string]string{
		"schedule": scheduleSpec,
	})
}

// Shutdown gracefully shuts down the hub.
func (h *Hub) Shutdown() {
	close(h.done)
}

// ClientCount returns the number of connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
