package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"lan-drop/internal/discovery"
	"lan-drop/internal/protocol"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: client <username> [serverAddr]")
	}

	// colorprofile v0.4+ queries the terminal via OSC sequences to detect
	// color support; terminals that don't respond (e.g. Termux) cause a hang.
	// Setting COLORTERM bypasses the active query.
	if os.Getenv("COLORTERM") == "" {
		os.Setenv("COLORTERM", "truecolor")
	}
	username := os.Args[1]

	var serverAddr string
	if len(os.Args) >= 3 {
		serverAddr = os.Args[2]
	} else {
		fmt.Println("searching for server...")
		addr, err := discovery.FindServer(3 * time.Second)
		if err != nil {
			log.Fatal("server not found – run: client <username> <host:port>")
		}
		serverAddr = addr
	}

	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	fmt.Fprintln(conn, username)

	eventCh := make(chan netEvent, 64)
	go startNetworkReader(conn, username, eventCh)

	p := tea.NewProgram(
		newModel(conn, username, serverAddr, eventCh),
		tea.WithInput(os.Stdin),
		tea.WithOutput(os.Stdout),
	)
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

func sendFile(conn net.Conn, username, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	fmt.Fprint(conn, protocol.FileHeader{
		From:     username,
		Filename: info.Name(),
		Size:     info.Size(),
	}.Encode())

	_, err = io.Copy(conn, f)
	return err
}
