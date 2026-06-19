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
- **Private messages (DM)** — `/dm <user> <message>` delivers privately to a single person
- **Encrypted private rooms** — start a server with `--pass` and all traffic is sealed with AES-256-GCM; the password also authenticates joiners and is never sent in the clear
- **Public / private servers** — no password = public and open; password = private and encrypted
- **Server picker** — the client scans the LAN on startup and shows a pick-list of servers (name · public/private · address), so you rarely type an IP; recently-used servers are remembered
- **File transfers with progress** — send any file (drag a file onto the terminal, or type its path); a live progress bar shows on both sender and receivers
- **Persistent scrollback** — chat history is saved per server and reloaded when you reconnect
- **Typing indicators** — animated dots show when someone is composing a message
- **Online users sidebar** — live list of who is currently connected, shown beside the chat
- **Color-coded names** — each username gets a consistent color derived from the name itself
- **Two-tab TUI** — `Chat` for messaging, `Files` for reviewing received files and sending new ones
- **Zero runtime configuration** — a single statically-linked binary per role (server / client)

## Requirements

| Requirement | Version |
|-------------|---------|
| Go | 1.24 or newer (only to build) |
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

The simplest flow needs only the **client** — you can host a room straight from the TUI:

```bash
./client alice
```

With no address the client scans the LAN and shows a **picker**. From there you can:

- **➕ Create a room (host)** — type a name, optionally a password (empty = public), and the client starts a server in-process and connects you to it. Others on the LAN now see your room in their picker.
- **Pick a discovered server** — choose with `↑/↓`, press `Enter`, and type the password if it's private (🔒).
- **Enter address manually** — for when discovery is blocked.

That's it: one person picks *Create a room*, everyone else picks it from the list.

### Dedicated / headless server (optional)

If you'd rather run a standalone server (e.g. on a machine nobody chats from):

```bash
./server                                 # public room on :8080
./server --name "huso's room"            # name shown in the client picker
./server --pass s3cret                   # private + AES-256-GCM encrypted room
./server --port 9090                     # custom port (or bare: ./server 9090)
```

### Skip the picker

```bash
./client alice 192.168.1.10:8080                 # public server by address
./client alice 192.168.1.10:8080 --pass s3cret   # private server by address
```

## Usage

### Chat tab (default)

| Action | Key / Input |
|--------|-------------|
| Type a message | Just start typing |
| Send message | `Enter` |
| Private message (one-off) | `/dm <user> <message>` (alias `/w`) |
| Private message (pick & pin) | `Ctrl+U`, choose a user with `↑/↓`, `Enter` — now every message you type goes to them |
| Leave DM mode | `Esc` (back to messaging everyone) |
| Send a file (drag & drop) | Drag a file onto the terminal, then `Enter` — the pasted path is sent as a file |
| Send a file (by path) | Type a file path and press `Enter` |
| Help / all shortcuts | `?` or `F1` |
| Switch to Files tab | `Tab` |
| Scroll up / down | `PgUp` / `PgDn` |
| Quit | `Ctrl+Q` or `Ctrl+C` |

When a DM target is pinned, the input prompt shows `[DM → name]` and the chosen user is marked in the sidebar. While a file is transferring, a progress bar appears just above the input line on both the sender and every receiver. Chat history is saved per server and restored (above a `── previous messages ──` divider) the next time you connect.

### Files tab

| Action | Key / Input |
|--------|-------------|
| Browse | `↑/↓` to move, `Enter` to open a folder or send a file, `⌫` to go up |
| Toggle hidden files | `.` |
| Help | `?` |
| Switch back to Chat | `Tab` |

The top of the Files tab shows a **Transfers** panel: active transfers with live progress bars, then recently received files (name, size, sender). Received files are saved to the directory where the client was launched.

### Sidebar

When the terminal is wider than 72 columns, a sidebar appears on the right side of the Chat tab showing all currently connected users. Your own name is tagged with `(you)`, and a pinned DM target is marked with `◆`.

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

**Server** (`internal/server`)  
The relay core, usable two ways: `Run` (blocking, used by the `cmd/server` binary) or `Start` (background goroutines, used when the client hosts a room from the TUI). Each connection registers a `Client` in the `Hub`; chat goes to peers (or one peer for a DM), files are relayed chunk-by-chunk, and the live user list is broadcast after every join/leave. `cmd/server` is now a thin flag-parsing wrapper.

**Hub** (`internal/hub`)  
Thread-safe registry of connected clients. Supports `Register`, `Unregister`, `Broadcast`, `BroadcastExcluding`, and `SendTo` (for direct messages). Sends are file-safe: a live client never has frames dropped, while a departed client never blocks the sender.

**Transport** (`internal/transport`)  
Wraps each TCP connection in **length-prefixed frames**. On a private room the payload is sealed with AES-256-GCM using a key derived from the room password via PBKDF2 (`crypto/pbkdf2`, stdlib); on a public room frames are sent in the clear. Because every frame is self-delimited, file bytes can be chunked and interleaved safely with chat traffic.

**Discovery** (`internal/discovery`)  
Server side: listens on UDP 9999, responds `LANDROP_HERE:<port>:<private>:<name>` to any `LANDROP_DISCOVER` probe.  
Client side: `FindServers` broadcasts a probe and collects **every** server that replies within the window, returning name, address, and public/private status.

**Protocol** (`internal/protocol`)  
Each frame carries one text message of the form:

```
TYPE:FROM:TO:BODY        (TO empty = broadcast; non-empty = direct message)
```

| Type | Direction | Meaning |
|------|-----------|---------|
| `MSG` | client → server → peers | Chat message (broadcast or DM via `TO`) |
| `JOIN` | server → all | User joined |
| `LEAVE` | server → all | User left |
| `TYPING` | client → server → peers | Typing indicator heartbeat |
| `USERLIST` | server → all | Comma-separated list of connected usernames |
| `FILE` | client → server → peers | File transfer header |

File transfer is a header frame followed by chunk frames:

```
FILE:<from>:<to>:<id>:<size_bytes>:<filename>     header frame
CHUNK:<id>:<raw bytes>                            one or more chunk frames
```

The handshake (first frame a client sends) is `HELLO:<username>`. On a private room it is encrypted, so the server accepting it proves the client knows the password.

**Client TUI** (`cmd/client`)  
Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) (Elm-style architecture), [Bubbles](https://github.com/charmbracelet/bubbles) (text input, viewport), and [Lipgloss](https://github.com/charmbracelet/lipgloss) (styling). A background goroutine reads from the TCP connection and forwards events into a channel consumed by the TUI update loop.

## Project Structure

```
lan-drop/
├── cmd/
│   ├── server/
│   │   └── main.go          # Thin standalone-server entry point (flags)
│   └── client/
│       ├── main.go          # Client entry point, connect/handshake
│       ├── picker.go        # LAN server picker + password prompt
│       ├── config.go        # Recently-used servers (~/.config/chokuto)
│       ├── history.go       # Persistent scrollback (~/.local/share/chokuto)
│       └── tui.go           # Bubble Tea model, views, network reader
├── internal/
│   ├── discovery/
│   │   └── discovery.go     # UDP auto-discovery (server + client sides)
│   ├── server/
│   │   └── server.go        # Relay core (Run blocking / Start in-process)
│   ├── transport/
│   │   └── transport.go     # Framed I/O + AES-256-GCM encryption
│   ├── hub/
│   │   └── hub.go           # Thread-safe client registry and broadcast
│   ├── protocol/
│   │   └── message.go       # Message types, encode/decode helpers
│   └── e2e/
│       └── e2e_test.go      # Integration tests (server + clients over TCP)
├── go.mod
└── go.sum
```

## Configuration Reference

| Parameter | Default | How to change |
|-----------|---------|---------------|
| Server name | hostname | `./server --name "..."` |
| Server password / privacy | none (public) | `./server --pass "..."` |
| Server TCP port | `8080` | `./server --port <port>` (or `./server <port>`) |
| Discovery UDP port | `9999` | Hardcoded in `internal/discovery/discovery.go` |
| Discovery timeout | `2.5s` | Hardcoded in `cmd/client/main.go` (`FindServers` call) |
| Recently-used servers | `~/.config/chokuto/servers.json` | `XDG_CONFIG_HOME` |
| Saved chat history | `~/.local/share/chokuto/history/` | `XDG_DATA_HOME` |

> **Security note:** encryption is keyed off the room password with a fixed salt and is intended for protecting LAN traffic from passive sniffers — not as a hardened scheme for the public internet. Private-room history is written to disk in plain text.

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
| [charmbracelet/bubbles](https://github.com/charmbracelet/bubbles) | Text input, viewport, and progress bar components |
| [charmbracelet/lipgloss](https://github.com/charmbracelet/lipgloss) | Terminal color and layout styling |

Room encryption (AES-256-GCM + PBKDF2 key derivation) uses only the Go standard library — no third-party crypto dependency.

## License

MIT
