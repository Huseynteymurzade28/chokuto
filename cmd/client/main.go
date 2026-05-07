package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"lan-drop/internal/discovery"
	"lan-drop/internal/protocol"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: client <username> [serverAddr]")
	}
	username := os.Args[1]

	var serverAddr string
	if len(os.Args) >= 3 {
		serverAddr = os.Args[2]
	} else {
		fmt.Println("searching for server...")
		addr, err := discovery.FindServer(3 * time.Second)
		if err != nil {
			log.Fatal("server not found, enter manually: client <username> <serverAddr>")
		}
		fmt.Println("server found:", addr)
		serverAddr = addr
	}

	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	fmt.Fprintln(conn, username)

	go readLoop(conn)

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "/send ") {
			filePath := strings.TrimPrefix(line, "/send ")
			err := sendFile(conn, username, filePath)
			if err != nil {
				log.Println("file send failed:", err)
			}
			continue
		}

		msg := protocol.Message{
			Type: protocol.TypeMessage,
			From: username,
			Body: line,
		}
		fmt.Fprint(conn, msg.Encode())
	}
}

func readLoop(conn net.Conn) {
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			log.Println("connection closed")
			os.Exit(0)
		}

		msg, err := protocol.Decode(line)
		if err != nil {
			continue
		}

		if msg.Type == protocol.TypeFile {
			parts := strings.SplitN(msg.Body, ":", 2)
			if len(parts) != 2 {
				continue
			}
			filename := parts[0]
			var size int64
			fmt.Sscanf(parts[1], "%d", &size)

			buf := make([]byte, size)
			_, err := io.ReadFull(reader, buf)
			if err != nil {
				log.Println("failed to read file:", err)
				continue
			}

			err = os.WriteFile(filename, buf, 0644)
			if err != nil {
				log.Println("failed to save file:", err)
				continue
			}

			fmt.Printf("*** file received: %s (%d bytes)\n", filename, size)
			continue
		}

		switch msg.Type {
		case protocol.TypeJoin:
			fmt.Printf("*** %s\n", msg.Body)
		case protocol.TypeLeave:
			fmt.Printf("*** %s\n", msg.Body)
		case protocol.TypeMessage:
			fmt.Printf("[%s]: %s\n", msg.From, msg.Body)
		}
	}
}

func sendFile(conn net.Conn, username, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	fh := protocol.FileHeader{
		From:     username,
		Filename: info.Name(),
		Size:     info.Size(),
	}

	fmt.Fprint(conn, fh.Encode())

	_, err = io.Copy(conn, f)
	return err
}
