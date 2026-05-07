package hub

import (
	"net"
)

type Client struct {
	Username string
	Send     chan []byte
	Conn     net.Conn
}

type envelope struct {
	data    []byte
	exclude *Client
}

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan envelope
	register   chan *Client
	unregister chan *Client
}

func New() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan envelope, 64),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *Hub) Register(c *Client) {
	h.register <- c
}

func (h *Hub) Unregister(c *Client) {
	h.unregister <- c
}

func (h *Hub) Broadcast(data []byte) {
	h.broadcast <- envelope{data: data}
}

func (h *Hub) BroadcastExcluding(data []byte, exclude *Client) {
	h.broadcast <- envelope{data: data, exclude: exclude}
}

func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.clients[c] = true

		case c := <-h.unregister:
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				close(c.Send)
			}

		case env := <-h.broadcast:
			for c := range h.clients {
				if env.exclude != nil && c == env.exclude {
					continue
				}
				c.Send <- env.data
			}
		}
	}
}
