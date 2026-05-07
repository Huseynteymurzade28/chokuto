package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"

	"lan-drop/internal/protocol"
)

const serverAddr = "localhost:8080"

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: client <username>")
	}
	username := os.Args[1]
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	fmt.Fprintln(conn, username)

	go func() {
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			msg, err := protocol.Decode(scanner.Text())
			if err != nil {
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
	}()

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		msg := protocol.Message{
			Type:  protocol.TypeMessage,
			From:  username,
			Body:  line,
		}
		fmt.Fprint(conn, msg.Encode())
	}
}
