package hub

import (
	"sync"

	"lan-drop/internal/transport"
)

type Client struct {
	Username string
	Send     chan []byte
	Done     chan struct{} // closed when the client is unregistered
	Conn     *transport.Conn
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
		close(c.Done)
	}
	h.mu.Unlock()
}

// HasUser reports whether a username is currently connected.
func (h *Hub) HasUser(username string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if c.Username == username {
			return true
		}
	}
	return false
}

// snapshot returns the current clients, optionally excluding one or filtering by
// username. Taken under the read lock so sends can happen lock-free afterwards.
func (h *Hub) snapshot(exclude *Client, onlyUser string) []*Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		if c == exclude {
			continue
		}
		if onlyUser != "" && c.Username != onlyUser {
			continue
		}
		out = append(out, c)
	}
	return out
}

// Broadcast reliably delivers data to every client (file-safe: it does not drop
// frames, it blocks per-client until the writer accepts or the client leaves).
func (h *Hub) Broadcast(data []byte) {
	for _, c := range h.snapshot(nil, "") {
		send(c, data)
	}
}

func (h *Hub) BroadcastExcluding(data []byte, exclude *Client) {
	for _, c := range h.snapshot(exclude, "") {
		send(c, data)
	}
}

// SendTo delivers data to every connection owned by username. Returns true if
// at least one recipient was found.
func (h *Hub) SendTo(username string, data []byte) bool {
	clients := h.snapshot(nil, username)
	for _, c := range clients {
		send(c, data)
	}
	return len(clients) > 0
}

// send blocks until the writer accepts the frame or the client leaves; it never
// panics on a departed client and never drops a frame for a live one.
func send(c *Client, data []byte) {
	select {
	case c.Send <- data:
	case <-c.Done:
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
