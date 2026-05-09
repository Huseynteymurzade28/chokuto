package hub

import (
	"net"
	"sync"
)

type Client struct {
	Username string
	Send     chan []byte
	Conn     net.Conn
}

type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]bool
}

func New() *Hub {
	return &Hub{clients: make(map[*Client]bool)}
}

func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
}

func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.Send)
	}
	h.mu.Unlock()
}

func (h *Hub) Broadcast(data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		select {
		case c.Send <- data:
		default:
		}
	}
}

func (h *Hub) BroadcastExcluding(data []byte, exclude *Client) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if c == exclude {
			continue
		}
		select {
		case c.Send <- data:
		default:
		}
	}
}

func (h *Hub) GetUsernames() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	names := make([]string, 0, len(h.clients))
	for c := range h.clients {
		names = append(names, c.Username)
	}
	return names
}
