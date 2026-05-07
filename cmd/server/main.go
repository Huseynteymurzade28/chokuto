package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"

	"lan-drop/internal/discovery"
	"lan-drop/internal/hub"
	"lan-drop/internal/protocol"
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

	fmt.Println("server started on", port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("accept error: %v", err)
			continue
		}
		go handleConn(conn, h)
	}
}

func handleConn(conn net.Conn, h *hub.Hub) {
	defer conn.Close()

	reader := bufio.NewReader(conn)

	line, err := reader.ReadString('\n')
	if err != nil {
		return
	}
	username := strings.TrimSpace(line)

	client := &hub.Client{
		Username: username,
		Send:     make(chan []byte, 32),
		Conn:     conn,
	}

	h.Register(client)
	defer h.Unregister(client)

	h.Broadcast([]byte(protocol.Message{
		Type: protocol.TypeJoin,
		From: username,
		Body: username + " joined",
	}.Encode()))

	go func() {
		for data := range client.Send {
			conn.Write(data)
		}
	}()

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		msg, err := protocol.Decode(line)
		if err != nil {
			continue
		}

		if msg.Type == protocol.TypeFile {
			handleFile(client, reader, h, line)
			continue
		}

		h.Broadcast([]byte(msg.Encode()))
	}

	h.Broadcast([]byte(protocol.Message{
		Type: protocol.TypeLeave,
		From: username,
		Body: username + " left",
	}.Encode()))
}

func handleFile(client *hub.Client, reader *bufio.Reader, h *hub.Hub, headerLine string) {
	fh, err := protocol.DecodeFileHeader(headerLine)
	if err != nil {
		return
	}

	buf := make([]byte, fh.Size)
	_, err = io.ReadFull(reader, buf)
	if err != nil {
		return
	}

	data := append([]byte(fh.Encode()), buf...)
	h.BroadcastExcluding(data, client)
}
