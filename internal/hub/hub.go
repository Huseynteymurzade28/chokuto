package hub

import "lan-drop/internal/protocol"


type Client struct {
	Username string
	Send chan protocol.Message
}

type Hub struct {
	clients map[*Client]bool
	broadcast chan protocol.Message
	register chan *Client
	unregister chan *Client
}

func New() *Hub {
	return &Hub{
		clients: make(map[*Client]bool),
		broadcast: make(chan protocol.Message),
		register: make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *Hub) Register(c *Client) {
	h.register <- c
}

func (h *Hub) Unregister(c *Client) {
	h.unregister <- c
}

func (h *Hub) Broadcast(m protocol.Message) {
	h.broadcast <- m
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
			case msg := <-h.broadcast:
				for c := range h.clients {
					c.Send <- msg
				}
		}
	}
}
