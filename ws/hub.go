package ws

import (
	"encoding/json"
	"log/slog"
	"sync"
)

// Hub manages all WebSocket connections and broadcasts messages.
type Hub struct {
	clients map[int64]*SafeConn
	mu      sync.RWMutex
}

// GlobalHub is the application-wide WebSocket broadcast hub.
var GlobalHub = &Hub{clients: make(map[int64]*SafeConn)}

// Register adds a connection to the hub.
func (h *Hub) Register(conn *SafeConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[conn.ID] = conn
}

// Unregister removes a connection from the hub and closes it.
func (h *Hub) Unregister(conn *SafeConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[conn.ID]; ok {
		delete(h.clients, conn.ID)
		conn.Close()
	}
}

// Broadcast JSON-marshals msg and sends it to all registered clients.
// Clients that fail to receive are silently removed.
func (h *Hub) Broadcast(msg any) {
	data, err := json.Marshal(msg)
	if err != nil {
		slog.Warn("ws hub: failed to marshal broadcast message", "error", err)
		return
	}

	h.mu.RLock()
	clients := make([]*SafeConn, 0, len(h.clients))
	for _, c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	var failed []int64
	for _, c := range clients {
		if err := c.WriteMessage(1, data); err != nil { // 1 = TextMessage
			failed = append(failed, c.ID)
		}
	}

	if len(failed) > 0 {
		h.mu.Lock()
		for _, id := range failed {
			delete(h.clients, id)
		}
		h.mu.Unlock()
	}
}
