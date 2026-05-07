package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"

	"lan-drop/internal/hub"
	"lan-drop/internal/protocol"
	"lan-drop/internal/discovery"
)

func main() {
	port := ":8080"
	if len(os.Args) > 1 {
		port = ":" + os.Args[1]
	}

	h := hub.New()
	go h.Run()
	go discovery.ListenAndRespond("8080")

	listener, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()

	fmt.Println("Server Started!", port)

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
		Conn:     conn,
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

		if msg.Type == protocol.TypeFile {
			handleFile(conn, h, line)
			continue
		}

		h.Broadcast(msg)
	}

	h.Broadcast(protocol.Message{
		Type: protocol.TypeLeave,
		From: username,
		Body: username + " left",
	})
}

func handleFile(conn net.Conn, h *hub.Hub, headerLine string) {
	fh, err := protocol.DecodeFileHeader(headerLine)
	if err != nil {
		log.Printf("failed to decode file header: %v", err)
		return
	}

	buf := make([]byte, fh.Size)
	_, err = io.ReadFull(conn, buf)
	if err != nil {
		log.Printf("failed to read file data: %v", err)
		return
	}

	clients := h.Clients()
	for _, c := range clients {
		if c.Conn == conn {
			continue
		}
		fmt.Fprint(c.Conn, fh.Encode())
		c.Conn.Write(buf)
	}

	log.Printf("file transfer done: %s (%d bytes)", fh.Filename, fh.Size)
}
