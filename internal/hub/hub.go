package hub

import (
	"net"
	"lan-drop/internal/protocol"
)

type Client struct {
	Username string
	Send     chan protocol.Message
	Conn     net.Conn
}

type Hub struct {
	clients      map[*Client]bool
	broadcast    chan protocol.Message
	register     chan *Client
	unregister   chan *Client
	clientsQuery chan chan []*Client
}

func New() *Hub {
	return &Hub{
		clients:      make(map[*Client]bool),
		broadcast:    make(chan protocol.Message),
		register:     make(chan *Client),
		unregister:   make(chan *Client),
		clientsQuery: make(chan chan []*Client),
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

func (h *Hub) Clients() []*Client {
	result := make(chan []*Client)
	h.clientsQuery <- result
	return <-result
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

		case result := <-h.clientsQuery:
			list := make([]*Client, 0, len(h.clients))
			for c := range h.clients {
				list = append(list, c)
			}
			result <- list
		}
	}
}