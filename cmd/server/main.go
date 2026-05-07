package main

import (
	"bufio"
	"fmt"
	"log"
	"net"

	"lan-drop/internal/hub"
	"lan-drop/internal/protocol"
)

const port = ":8080"

func main() {
	h := hub.New()
	go h.Run()

	listener, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()

	fmt.Println("Server Started!",port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("failed to accept: %v", err)
			continue
		}
		go handleConn(conn, h)
	}
	
}
func handleConn(conn net.Conn, h *hub.Hub) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		return
	}
	username := scanner.Text()
	client := &hub.Client{
		Username: username,
		Send:     make(chan protocol.Message, 32),
	}
	h.Register(client)
	defer h.Unregister(client)

	h.Broadcast(protocol.Message{
		Type: protocol.TypeJoin,
		From: username,
		Body: username + " joined",
	})

	go func() {
		for msg := range client.Send {
			fmt.Fprint(conn, msg.Encode())
		}
	}()

	for scanner.Scan() {
		line := scanner.Text()
		msg, err := protocol.Decode(line)
		if err != nil {
			log.Printf("failed to decode: %v", err)
			continue
		}
		h.Broadcast(msg)
	}

	h.Broadcast(protocol.Message{
		Type: protocol.TypeLeave,
		From: username,
		Body: username + " leaved",
	})
}
