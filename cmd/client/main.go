package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"lan-drop/internal/discovery"
	"lan-drop/internal/protocol"
	"lan-drop/internal/server"
	"lan-drop/internal/transport"
)

func main() {
	// Parse args manually so --pass works in any position (flag.Parse stops at
	// the first positional argument).
	args, passFlag := parseArgs(os.Args[1:])
	if len(args) < 1 {
		log.Fatal("usage: client <username> [serverAddr] [--pass password]")
	}

	// colorprofile v0.4+ sends an OSC query to detect color support; Termux does
	// not respond, causing a hang. Force-set to bypass the query.
	os.Setenv("COLORTERM", "truecolor")
	username := args[0]
	if strings.ContainsAny(username, ":,") {
		log.Fatal("username cannot contain ':' or ','")
	}

	var addr, password, displayName string
	remember := true // don't remember ad-hoc loopback host sessions
	if len(args) >= 2 {
		addr = args[1]
		if !strings.Contains(addr, ":") {
			addr += ":8080"
		}
		password = passFlag
		displayName = addr
	} else {
		fmt.Println("searching for servers on the LAN...")
		found := discovery.FindServers(2500 * time.Millisecond)
		res, err := runPicker(username, found, loadRecents())
		if err != nil {
			log.Fatal(err)
		}
		if res.cancelled {
			return
		}
		if res.host {
			// Host a room in-process, then connect to it as a regular client.
			inst, err := server.Start(server.Options{Name: res.resName, Pass: res.resPass})
			if err != nil {
				log.Fatalf("could not host room: %v", err)
			}
			defer inst.Stop()
			addr = inst.Addr
			remember = false
		} else {
			addr = res.resAddr
		}
		password = res.resPass
		displayName = res.resName
	}
	if displayName == "" {
		displayName = addr
	}

	var key []byte
	if password != "" {
		k, err := transport.DeriveKey(password)
		if err != nil {
			log.Fatalf("key derivation failed: %v", err)
		}
		key = k
	}

	rawConn, err := net.DialTimeout("tcp", addr, 8*time.Second)
	if err != nil {
		log.Fatalf("could not connect to %s: %v\n(are both devices on the same Wi-Fi? is the room running on that address?)", addr, err)
	}
	defer rawConn.Close()

	conn, err := transport.NewConn(rawConn, key)
	if err != nil {
		log.Fatal(err)
	}

	// Handshake frame. On a private server this is encrypted; if our password is
	// wrong the server drops us and the preflight read below fails.
	if err := conn.WriteFrame([]byte(protocol.EncodeAuth(username))); err != nil {
		log.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.ReadFrame(); err != nil {
		log.Fatalf("connection refused by %s (wrong password?)", addr)
	}
	conn.SetReadDeadline(time.Time{})

	if remember {
		rememberServer(recentServer{Name: displayName, Addr: addr, Private: password != ""})
	}

	eventCh := make(chan netEvent, 64)
	progressCh := make(chan transferMsg, 64)
	go startNetworkReader(conn, username, eventCh)

	history := loadHistory(addr)

	p := tea.NewProgram(
		newModel(conn, username, addr, password != "", history, eventCh, progressCh),
		tea.WithAltScreen(),
		tea.WithInput(os.Stdin),
		tea.WithOutput(os.Stdout),
	)
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

// parseArgs splits the command line into positional args and the --pass value,
// accepting --pass anywhere (--pass x, --pass=x, -pass x).
func parseArgs(in []string) (positional []string, pass string) {
	for i := 0; i < len(in); i++ {
		a := in[i]
		switch {
		case a == "--pass" || a == "-pass":
			if i+1 < len(in) {
				pass = in[i+1]
				i++
			}
		case strings.HasPrefix(a, "--pass="):
			pass = strings.TrimPrefix(a, "--pass=")
		case strings.HasPrefix(a, "-pass="):
			pass = strings.TrimPrefix(a, "-pass=")
		default:
			positional = append(positional, a)
		}
	}
	return positional, pass
}

// runPicker runs the server selection screen and returns the chosen target.
func runPicker(username string, found []discovery.ServerInfo, recents []recentServer) (pickerModel, error) {
	p := tea.NewProgram(newPicker(username, found, recents), tea.WithAltScreen())
	m, err := p.Run()
	if err != nil {
		return pickerModel{}, err
	}
	return m.(pickerModel), nil
}
