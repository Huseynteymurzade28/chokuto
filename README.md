# lan-drop

A lightweight LAN chat and file-sharing tool that runs entirely in your terminal. No internet required, no accounts, no configuration files — just launch the server on one machine and connect from any other machine on the same network.

```
lan-drop  │  Chat · Files                                  ● 3 online

15:42  ⊕ alice joined
15:43  bob › hey everyone
15:43  alice › hi! sending the build logs now
15:44  ⬇ alice: sent server.log (142.3 KB)
15:44  charlie is typing...
────────────────────────────────────────────────────────
 › _
 alice@192.168.1.10:8080        tab: switch  pgup/dn: scroll  ctrl+q: quit
```

## Features

- **Real-time group chat** — messages are broadcast to everyone on the session instantly
- **File transfers** — send any file by typing its path; all peers receive and save it automatically
- **Auto-discovery** — the client broadcasts a UDP probe and finds the server on its own; no IP address needed in most cases
- **Typing indicators** — animated dots show when someone is composing a message
- **Online users sidebar** — live list of who is currently connected, shown beside the chat
- **Color-coded names** — each username gets a consistent color derived from the name itself
- **Two-tab TUI** — `Chat` for messaging, `Files` for reviewing received files and sending new ones
- **Zero dependencies at runtime** — a single statically-linked binary per role (server / client)

## Requirements

| Requirement | Version |
|-------------|---------|
| Go | 1.21 or newer |
| OS | Linux, macOS, Windows (any terminal with ANSI support) |

Both machines must be on the **same local network** (same Wi-Fi, LAN, or VPN subnet).

## Installation

Clone and build both binaries:

```bash
git clone https://github.com/Huseynteymurzade28/lan-drop.git
cd lan-drop
go build -o server ./cmd/server
go build -o client ./cmd/client
```

Binaries will appear in the project root.

## Quick Start

### 1. Start the server (one machine)

```bash
./server
```

The server listens on TCP port **8080** by default and also opens UDP port **9999** for discovery.

To use a custom port:

```bash
./server 9090
```

### 2. Connect clients (any machine on the same network)

```bash
./client alice
```

The client automatically discovers the server via UDP broadcast. If discovery fails (e.g. broadcast is blocked on the network), provide the address manually:

```bash
./client alice 192.168.1.10:8080
```

Repeat this step on as many machines (or terminal windows) as you like. Everyone connects to the same server session.

## Usage

### Chat tab (default)

| Action | Key / Input |
|--------|-------------|
| Type a message | Just start typing |
| Send message | `Enter` |
| Switch to Files tab | `Tab` |
| Scroll up / down | `PgUp` / `PgDn` |
| Quit | `Ctrl+Q` or `Ctrl+C` |

### Files tab

| Action | Key / Input |
|--------|-------------|
| Send a file | Type the **full path** to the file and press `Enter` |
| Switch back to Chat | `Tab` |

When a file is sent, every connected client **receives and saves** it to the directory where they launched the client binary.

The Files tab also shows a log of all files received during the session (filename, size, sender, timestamp).

### Sidebar

When the terminal is wider than 60 columns, a sidebar appears on the right side of the Chat tab showing all currently connected users. Your own name is tagged with `(you)`.

## Architecture

```
┌─────────────────────────────────────────────────┐
│                     SERVER                      │
│                                                 │
│  TCP :8080  ←──────────────────→  hub.go        │
│  (accept connections, read/write messages)      │
│                                                 │
│  UDP :9999  ←── broadcast probe  discovery.go   │
│             ──→ LANDROP_HERE reply              │
└─────────────────────────────────────────────────┘
          ▲               ▲               ▲
          │ TCP           │ TCP           │ TCP
          ▼               ▼               ▼
      client A        client B        client C
      (TUI)           (TUI)           (TUI)
```

**Server** (`cmd/server`)  
Accepts TCP connections. Each new connection registers a `Client` in the `Hub`. Incoming messages and file transfers are broadcast to all other clients. Maintains and broadcasts the live user list after every join/leave.

**Hub** (`internal/hub`)  
Thread-safe registry of connected clients. Supports `Register`, `Unregister`, `Broadcast`, and `BroadcastExcluding`.

**Discovery** (`internal/discovery`)  
Server side: listens on UDP 9999, responds `LANDROP_HERE:<port>` to any `LANDROP_DISCOVER` probe.  
Client side: sends a UDP broadcast, waits up to 3 seconds for a reply, extracts the server address.

**Protocol** (`internal/protocol`)  
Newline-delimited text messages with the format:

```
TYPE:FROM:BODY\n
```

| Type | Direction | Meaning |
|------|-----------|---------|
| `MSG` | client → server → peers | Chat message |
| `JOIN` | server → all | User joined |
| `LEAVE` | server → all | User left |
| `TYPING` | client → server → peers | Typing indicator heartbeat |
| `USERLIST` | server → all | Comma-separated list of connected usernames |
| `FILE` | client → server → peers | File transfer (header line + raw bytes) |

File transfer wire format:

```
FILE:<from>:<filename>:<size_bytes>\n
<raw bytes>
```

**Client TUI** (`cmd/client`)  
Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) (Elm-style architecture), [Bubbles](https://github.com/charmbracelet/bubbles) (text input, viewport), and [Lipgloss](https://github.com/charmbracelet/lipgloss) (styling). A background goroutine reads from the TCP connection and forwards events into a channel consumed by the TUI update loop.

## Project Structure

```
lan-drop/
├── cmd/
│   ├── server/
│   │   └── main.go          # Server entry point
│   └── client/
│       ├── main.go          # Client entry point, file-send helper
│       └── tui.go           # Bubble Tea model, views, network reader
├── internal/
│   ├── discovery/
│   │   └── discovery.go     # UDP auto-discovery (server + client sides)
│   ├── hub/
│   │   └── hub.go           # Thread-safe client registry and broadcast
│   └── protocol/
│       └── message.go       # Message types, encode/decode helpers
├── go.mod
└── go.sum
```

## Configuration Reference

| Parameter | Default | How to change |
|-----------|---------|---------------|
| Server TCP port | `8080` | `./server <port>` |
| Discovery UDP port | `9999` | Hardcoded in `internal/discovery/discovery.go` |
| Discovery timeout | `3s` | Hardcoded in `cmd/client/main.go` (`FindServer` call) |
| Client send buffer | `64 messages` | Hardcoded in `cmd/server/main.go` (`hub.Client.Send` channel) |

## Troubleshooting

**Client says "server not found"**  
UDP broadcasts are sometimes blocked by routers or OS firewalls. Try providing the server address explicitly:
```bash
./client alice 192.168.1.10:8080
```

**Port already in use**  
Another process is using port 8080. Start the server on a different port and connect clients to that port:
```bash
./server 9090
./client alice 192.168.1.10:9090
```

**Files saved to wrong directory**  
Received files are written to the working directory of the client process, not the directory the binary lives in. Run the client from the folder where you want files to land:
```bash
cd ~/Downloads && /path/to/client alice
```

**TUI looks garbled**  
Ensure your terminal supports 256 colors and UTF-8. Most modern terminals (iTerm2, GNOME Terminal, Windows Terminal, Alacritty, kitty) work out of the box.

## Dependencies

| Library | Purpose |
|---------|---------|
| [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea) | TUI framework (Elm architecture) |
| [charmbracelet/bubbles](https://github.com/charmbracelet/bubbles) | Text input and scrollable viewport components |
| [charmbracelet/lipgloss](https://github.com/charmbracelet/lipgloss) | Terminal color and layout styling |

## License

MIT
