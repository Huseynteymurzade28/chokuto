package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"lan-drop/internal/protocol"
)

// ── styles ────────────────────────────────────────────────────────────────────

var (
	activeTabSt   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")).Underline(true).Padding(0, 1)
	inactiveTabSt = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Padding(0, 1)
	titleSt       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	dimSt         = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	joinSt        = lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Italic(true)
	leaveSt       = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Italic(true)
	otherSt       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("117"))
	meSt          = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	fileSt        = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	errSt         = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)

// ── types ─────────────────────────────────────────────────────────────────────

type tabIdx int

const (
	chatTab tabIdx = iota
	filesTab
)

type chatLine struct {
	kind string // me | msg | join | leave | file | err | sys
	from string
	body string
	ts   time.Time
}

type fileEntry struct {
	filename string
	size     int64
	from     string
	ts       time.Time
}

// netEvent is produced by the background network goroutine.
type netEvent struct {
	line chatLine
	file *fileEntry // non-nil when a file was received
}

type fileSentMsg struct {
	name string
	size int64
}

type errMsg struct{ err error }

// ── model ─────────────────────────────────────────────────────────────────────

type model struct {
	tab      tabIdx
	lines    []chatLine
	files    []fileEntry
	vp       viewport.Model
	input    textinput.Model
	conn     net.Conn
	username string
	server   string
	eventCh  chan netEvent
	width    int
	height   int
	ready    bool
}

func newModel(conn net.Conn, username, server string, eventCh chan netEvent) model {
	ti := textinput.New()
	ti.Placeholder = "message..."
	ti.Focus()
	ti.CharLimit = 1000
	return model{
		conn:     conn,
		username: username,
		server:   server,
		eventCh:  eventCh,
		input:    ti,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, waitNet(m.eventCh))
}

func waitNet(ch <-chan netEvent) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

// ── update ────────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		vpH := m.height - 5 // header(2) + input(2) + status(1)
		if vpH < 1 {
			vpH = 1
		}
		m.vp = viewport.New(m.width, vpH)
		m.vp.SetContent(m.chatContent())
		m.vp.GotoBottom()
		m.ready = true

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "ctrl+q":
			return m, tea.Quit

		case "tab":
			m.tab = tabIdx(1 - int(m.tab))
			if m.tab == chatTab {
				m.input.Placeholder = "message..."
			} else {
				m.input.Placeholder = "/path/to/file"
			}

		case "enter":
			val := strings.TrimSpace(m.input.Value())
			m.input.Reset()
			if val == "" {
				break
			}
			if m.tab == chatTab {
				fmt.Fprint(m.conn, protocol.Message{
					Type: protocol.TypeMessage,
					From: m.username,
					Body: val,
				}.Encode())
				m.lines = append(m.lines, chatLine{
					kind: "me", from: m.username, body: val, ts: time.Now(),
				})
				m.refreshVP()
			} else {
				cmds = append(cmds, m.doSendFile(val))
			}
		}

	case netEvent:
		if msg.file != nil {
			m.files = append(m.files, *msg.file)
			m.lines = append(m.lines, chatLine{
				kind: "file",
				from: msg.file.from,
				body: fmt.Sprintf("sent %s (%s)", msg.file.filename, fmtSize(msg.file.size)),
				ts:   msg.file.ts,
			})
		} else {
			m.lines = append(m.lines, msg.line)
		}
		m.refreshVP()
		cmds = append(cmds, waitNet(m.eventCh))

	case fileSentMsg:
		m.lines = append(m.lines, chatLine{
			kind: "file",
			from: m.username,
			body: fmt.Sprintf("sent %s (%s)", msg.name, fmtSize(msg.size)),
			ts:   time.Now(),
		})
		m.refreshVP()

	case errMsg:
		m.lines = append(m.lines, chatLine{kind: "err", body: msg.err.Error(), ts: time.Now()})
		m.refreshVP()
	}

	var vpCmd, inputCmd tea.Cmd
	m.vp, vpCmd = m.vp.Update(msg)
	m.input, inputCmd = m.input.Update(msg)
	cmds = append(cmds, vpCmd, inputCmd)

	return m, tea.Batch(cmds...)
}

// ── view ──────────────────────────────────────────────────────────────────────

func (m model) View() string {
	if !m.ready {
		return "connecting..."
	}
	return strings.Join([]string{
		m.viewHeader(),
		m.viewContent(),
		m.viewInput(),
		m.viewStatus(),
	}, "\n")
}

func (m model) viewHeader() string {
	var chatLabel, filesLabel string
	if m.tab == chatTab {
		chatLabel = activeTabSt.Render("Chat")
		filesLabel = inactiveTabSt.Render("Files")
	} else {
		chatLabel = inactiveTabSt.Render("Chat")
		filesLabel = activeTabSt.Render("Files")
	}
	left := titleSt.Render("lan-drop") + dimSt.Render("  │  ") + chatLabel + dimSt.Render("·") + filesLabel
	line := lipgloss.NewStyle().Width(m.width).Render(left)
	border := dimSt.Render(strings.Repeat("─", m.width))
	return line + "\n" + border
}

func (m model) viewContent() string {
	if m.tab == filesTab {
		return m.viewFiles()
	}
	return m.vp.View()
}

func (m model) viewFiles() string {
	var rows []string
	if len(m.files) == 0 {
		rows = append(rows,
			"",
			dimSt.Render("  no files received yet"),
			"",
			dimSt.Render("  type an absolute file path below and press Enter to send"),
		)
	} else {
		rows = append(rows, "", dimSt.Render("  received files:"), "")
		for _, f := range m.files {
			ts := dimSt.Render(f.ts.Format("15:04"))
			row := fmt.Sprintf("  %s  %s  %s  %s",
				ts,
				fileSt.Render(f.filename),
				dimSt.Render(fmtSize(f.size)),
				dimSt.Render("← "+f.from),
			)
			rows = append(rows, row)
		}
	}
	for len(rows) < m.vp.Height {
		rows = append(rows, "")
	}
	return strings.Join(rows[:m.vp.Height], "\n")
}

func (m model) viewInput() string {
	border := dimSt.Render(strings.Repeat("─", m.width))
	var prefix string
	if m.tab == filesTab {
		prefix = fileSt.Render(" send › ")
	} else {
		prefix = dimSt.Render(" › ")
	}
	return border + "\n" + prefix + m.input.View()
}

func (m model) viewStatus() string {
	left := dimSt.Render(" " + m.username + "@" + m.server)
	right := dimSt.Render("tab: switch  pgup/dn: scroll  ctrl+q: quit ")
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	return left + strings.Repeat(" ", gap) + right
}

// ── helpers ───────────────────────────────────────────────────────────────────

func (m *model) refreshVP() {
	if !m.ready {
		return
	}
	m.vp.SetContent(m.chatContent())
	m.vp.GotoBottom()
}

func (m model) chatContent() string {
	if len(m.lines) == 0 {
		return dimSt.Render("  no messages yet – start typing below")
	}
	var sb strings.Builder
	for _, l := range m.lines {
		sb.WriteString(renderChatLine(l))
		sb.WriteByte('\n')
	}
	return sb.String()
}

func renderChatLine(l chatLine) string {
	ts := dimSt.Render(l.ts.Format("15:04"))
	sep := dimSt.Render(" › ")
	switch l.kind {
	case "join":
		return ts + " " + joinSt.Render("⊕ "+l.body)
	case "leave":
		return ts + " " + leaveSt.Render("⊖ "+l.body)
	case "me":
		return ts + " " + meSt.Render(l.from) + sep + l.body
	case "msg":
		return ts + " " + otherSt.Render(l.from) + sep + l.body
	case "file":
		return ts + " " + fileSt.Render("⬇ "+l.from+": "+l.body)
	case "err":
		return ts + " " + errSt.Render("✗ "+l.body)
	default:
		return ts + " " + dimSt.Render(l.body)
	}
}

func (m model) doSendFile(path string) tea.Cmd {
	conn, username := m.conn, m.username
	return func() tea.Msg {
		if err := sendFile(conn, username, path); err != nil {
			return errMsg{err}
		}
		info, _ := os.Stat(path)
		var size int64
		if info != nil {
			size = info.Size()
		}
		return fileSentMsg{name: filepath.Base(path), size: size}
	}
}

func fmtSize(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// ── network goroutine ─────────────────────────────────────────────────────────

func startNetworkReader(conn net.Conn, username string, eventCh chan<- netEvent) {
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			eventCh <- netEvent{line: chatLine{kind: "err", body: "disconnected from server", ts: time.Now()}}
			return
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
			var size int64
			fmt.Sscanf(parts[1], "%d", &size)

			buf := make([]byte, size)
			if _, err := io.ReadFull(reader, buf); err != nil {
				eventCh <- netEvent{line: chatLine{kind: "err", body: "file receive failed", ts: time.Now()}}
				continue
			}

			filename := parts[0]
			if err := os.WriteFile(filename, buf, 0644); err != nil {
				eventCh <- netEvent{line: chatLine{kind: "err", body: "file save failed: " + err.Error(), ts: time.Now()}}
				continue
			}

			eventCh <- netEvent{file: &fileEntry{
				filename: filename,
				size:     size,
				from:     msg.From,
				ts:       time.Now(),
			}}
			continue
		}

		// skip echo of own messages
		if msg.Type == protocol.TypeMessage && msg.From == username {
			continue
		}

		switch msg.Type {
		case protocol.TypeJoin:
			eventCh <- netEvent{line: chatLine{kind: "join", from: msg.From, body: msg.Body, ts: time.Now()}}
		case protocol.TypeLeave:
			eventCh <- netEvent{line: chatLine{kind: "leave", from: msg.From, body: msg.Body, ts: time.Now()}}
		case protocol.TypeMessage:
			eventCh <- netEvent{line: chatLine{kind: "msg", from: msg.From, body: msg.Body, ts: time.Now()}}
		}
	}
}
