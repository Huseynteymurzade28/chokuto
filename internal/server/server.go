// Package server contains the chat relay: it accepts client connections, relays
// chat/DM/file traffic, and answers LAN discovery. It can run blocking (the
// standalone `server` binary) or in the background (hosting from the client TUI).
package server

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"lan-drop/internal/discovery"
	"lan-drop/internal/hub"
	"lan-drop/internal/protocol"
	"lan-drop/internal/transport"
)

// Options configures a server instance.
type Options struct {
	Name string
	Pass string
	Port string // "" or "0" picks a free port
}

// Instance is a running server.
type Instance struct {
	Name     string
	Addr     string // host:port clients connect to (loopback for in-process hosting)
	Port     string
	Private  bool
	listener net.Listener
}

// Start begins listening and serving in the background, returning once the
// listener is open. Call Stop to shut it down.
func Start(opts Options) (*Instance, error) {
	private := opts.Pass != ""
	var key []byte
	if private {
		k, err := transport.DeriveKey(opts.Pass)
		if err != nil {
			return nil, err
		}
		key = k
	}

	var ln net.Listener
	var err error
	if opts.Port == "" {
		// Prefer the well-known port so a manual "host:8080" connection is
		// predictable; fall back to any free port if 8080 is taken.
		if ln, err = net.Listen("tcp", ":8080"); err != nil {
			ln, err = net.Listen("tcp", ":0")
		}
	} else {
		ln, err = net.Listen("tcp", ":"+opts.Port)
	}
	if err != nil {
		return nil, err
	}
	actualPort := fmt.Sprint(ln.Addr().(*net.TCPAddr).Port)

	inst := &Instance{
		Name:     opts.Name,
		Addr:     "127.0.0.1:" + actualPort,
		Port:     actualPort,
		Private:  private,
		listener: ln,
	}

	h := hub.New()
	go discovery.ListenAndRespond(actualPort, opts.Name, private)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			go handleConn(conn, h, key)
		}
	}()
	return inst, nil
}

// Stop shuts down the listener.
func (i *Instance) Stop() {
	if i.listener != nil {
		i.listener.Close()
	}
}

// Run starts a server and blocks forever (used by the standalone binary).
func Run(opts Options) error {
	inst, err := Start(opts)
	if err != nil {
		return err
	}
	mode := "public"
	if inst.Private {
		mode = "private (encrypted)"
	}
	fmt.Printf("server %q started on :%s [%s]\n", inst.Name, inst.Port, mode)
	select {} // block forever
}

func broadcastUserList(h *hub.Hub) {
	names := h.GetUsernames()
	sort.Strings(names)
	h.Broadcast([]byte(protocol.Message{
		Type: protocol.TypeUserList,
		From: "server",
		Body: strings.Join(names, ","),
	}.Encode()))
}

func handleConn(rawConn net.Conn, h *hub.Hub, key []byte) {
	tc, err := transport.NewConn(rawConn, key)
	if err != nil {
		rawConn.Close()
		return
	}
	defer tc.Close()

	// First frame is the handshake. On a private room it is encrypted, so a
	// failure here means the client used the wrong password (or no password).
	frame, err := tc.ReadFrame()
	if err != nil {
		return
	}
	username, ok := protocol.DecodeAuth(string(frame))
	if !ok || username == "" || strings.ContainsAny(username, ":,") {
		return
	}

	client := &hub.Client{
		Username: username,
		Send:     make(chan []byte, 256),
		Done:     make(chan struct{}),
		Conn:     tc,
	}

	h.Register(client)
	defer func() {
		h.Unregister(client)
		h.Broadcast([]byte(protocol.Message{
			Type: protocol.TypeLeave,
			From: username,
			Body: username + " left",
		}.Encode()))
		broadcastUserList(h)
	}()

	h.Broadcast([]byte(protocol.Message{
		Type: protocol.TypeJoin,
		From: username,
		Body: username + " joined",
	}.Encode()))
	broadcastUserList(h)

	// Writer goroutine: drain Send to the encrypted/framed connection.
	go func() {
		for {
			select {
			case data := <-client.Send:
				if err := tc.WriteFrame(data); err != nil {
					tc.Close()
					return
				}
			case <-client.Done:
				return
			}
		}
	}()

	// routes maps an in-flight transfer ID to its destination ("" = broadcast);
	// remaining tracks bytes still expected so we can forget the route when done.
	routes := make(map[string]string)
	remaining := make(map[string]int64)

	relay := func(data []byte, to string) {
		if to == "" {
			h.BroadcastExcluding(data, client)
		} else {
			h.SendTo(to, data)
		}
	}

	for {
		frame, err := tc.ReadFrame()
		if err != nil {
			break
		}

		// File body chunk.
		if protocol.IsChunk(frame) {
			id, data, ok := protocol.DecodeChunk(frame)
			if !ok {
				continue
			}
			to, known := routes[id]
			if !known {
				continue
			}
			relay(frame, to)
			remaining[id] -= int64(len(data))
			if remaining[id] <= 0 {
				delete(routes, id)
				delete(remaining, id)
			}
			continue
		}

		s := string(frame)

		// File header: record routing, then relay the header.
		if strings.HasPrefix(s, "FILE:") {
			fh, err := protocol.DecodeFileHeader(s)
			if err != nil {
				continue
			}
			routes[fh.ID] = fh.To
			remaining[fh.ID] = fh.Size
			relay(frame, fh.To)
			continue
		}

		// Control / chat message.
		msg, err := protocol.Decode(s)
		if err != nil {
			continue
		}
		msg.From = username // trust the connection's identity
		if msg.To != "" {
			// Direct message: deliver only to the target. The sender renders its
			// own copy locally, so no echo is needed.
			h.SendTo(msg.To, []byte(msg.Encode()))
		} else {
			h.BroadcastExcluding([]byte(msg.Encode()), client)
		}
	}
}
