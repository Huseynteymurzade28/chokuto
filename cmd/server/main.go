package main

import (
	"flag"
	"log"
	"os"

	"lan-drop/internal/server"
)

func main() {
	defaultName, _ := os.Hostname()
	if defaultName == "" {
		defaultName = "chokuto"
	}

	name := flag.String("name", defaultName, "server name shown to clients")
	pass := flag.String("pass", "", "room password; if set the room is private and encrypted")
	port := flag.String("port", "8080", "TCP port to listen on")
	flag.Parse()
	// Allow a bare positional port for backwards compatibility: `server 9000`.
	if args := flag.Args(); len(args) > 0 {
		*port = args[0]
	}

	if err := server.Run(server.Options{Name: *name, Pass: *pass, Port: *port}); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
