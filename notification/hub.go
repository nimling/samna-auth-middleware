package notification

import (
	"sync"

	"github.com/google/uuid"
)

type Client struct {
	Send chan []byte
}

type hub struct {
	mu      sync.RWMutex
	clients map[uuid.UUID]map[*Client]bool
}

func newHub() *hub {
	return &hub{clients: make(map[uuid.UUID]map[*Client]bool)}
}

func (h *hub) register(userID uuid.UUID) *Client {
	c := &Client{Send: make(chan []byte, clientBuffer)}
	h.mu.Lock()
	if h.clients[userID] == nil {
		h.clients[userID] = make(map[*Client]bool)
	}
	h.clients[userID][c] = true
	h.mu.Unlock()
	return c
}

func (h *hub) unregister(userID uuid.UUID, c *Client) {
	h.mu.Lock()
	if set := h.clients[userID]; set != nil {
		if set[c] {
			delete(set, c)
			close(c.Send)
		}
		if len(set) == 0 {
			delete(h.clients, userID)
		}
	}
	h.mu.Unlock()
}

func (h *hub) has(userID uuid.UUID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients[userID]) > 0
}

func (h *hub) push(userID uuid.UUID, data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients[userID] {
		select {
		case c.Send <- data:
		default:
		}
	}
}

func (h *hub) closeAll() {
	h.mu.Lock()
	for userID, set := range h.clients {
		for c := range set {
			close(c.Send)
		}
		delete(h.clients, userID)
	}
	h.mu.Unlock()
}
