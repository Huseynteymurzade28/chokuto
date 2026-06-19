package main

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"lan-drop/internal/discovery"
)

type pickerEntry struct {
	name    string
	addr    string
	private bool
	online  bool // discovered on the LAN right now
	manual  bool // the "enter address manually" row
	create  bool // the "create a room" row
}

const (
	pickList = iota
	pickAddr
	pickPass
	pickCreateName
)

type pickerModel struct {
	username string
	entries  []pickerEntry
	cursor   int
	mode     int
	input    textinput.Model

	pendName    string
	pendAddr    string
	pendPrivate bool
	pendHost    bool

	// result
	done       bool
	cancelled  bool
	host       bool
	resAddr    string
	resName    string
	resPass    string
	resPrivate bool
}

func newPicker(username string, found []discovery.ServerInfo, recents []recentServer) pickerModel {
	entries := []pickerEntry{{name: "Create a room (host)", create: true}}
	seen := make(map[string]bool)
	for _, s := range found {
		entries = append(entries, pickerEntry{name: s.Name, addr: s.Addr, private: s.Private, online: true})
		seen[s.Addr] = true
	}
	for _, r := range recents {
		if seen[r.Addr] {
			continue
		}
		entries = append(entries, pickerEntry{name: r.Name, addr: r.Addr, private: r.Private})
	}
	entries = append(entries, pickerEntry{name: "enter address manually…", manual: true})

	ti := textinput.New()
	ti.CharLimit = 256
	return pickerModel{username: username, entries: entries, input: ti}
}

func (m pickerModel) Init() tea.Cmd { return textinput.Blink }

func (m *pickerModel) finish(addr, name string, private bool, pass string) {
	m.done = true
	m.resAddr = addr
	m.resName = name
	m.resPrivate = private
	m.resPass = pass
}

func (m *pickerModel) finishHost(name, pass string) {
	m.done = true
	m.host = true
	m.resName = name
	m.resPass = pass
	m.resPrivate = pass != ""
}

func (m *pickerModel) startPass(placeholder string) tea.Cmd {
	m.mode = pickPass
	m.input.Placeholder = placeholder
	m.input.EchoMode = textinput.EchoPassword
	m.input.SetValue("")
	m.input.Focus()
	return textinput.Blink
}

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch m.mode {
	case pickList:
		switch key.String() {
		case "ctrl+c", "q", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.entries)-1 {
				m.cursor++
			}
		case "enter":
			e := m.entries[m.cursor]
			switch {
			case e.create:
				m.mode = pickCreateName
				m.input.Placeholder = "room name"
				m.input.EchoMode = textinput.EchoNormal
				m.input.SetValue(m.username + "'s room")
				m.input.CursorEnd()
				m.input.Focus()
				return m, textinput.Blink
			case e.manual:
				m.mode = pickAddr
				m.input.Placeholder = "host:port  (e.g. 192.168.1.10:8080)"
				m.input.EchoMode = textinput.EchoNormal
				m.input.SetValue("")
				m.input.Focus()
				return m, textinput.Blink
			case e.private:
				m.pendName, m.pendAddr, m.pendPrivate, m.pendHost = e.name, e.addr, true, false
				return m, m.startPass("password")
			default:
				m.finish(e.addr, e.name, false, "")
				return m, tea.Quit
			}
		}

	case pickCreateName:
		switch key.String() {
		case "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		case "esc":
			m.mode = pickList
			return m, nil
		case "enter":
			name := strings.TrimSpace(m.input.Value())
			if name == "" {
				name = m.username + "'s room"
			}
			m.pendName, m.pendHost = name, true
			return m, m.startPass("password (leave empty for public)")
		}

	case pickAddr:
		switch key.String() {
		case "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		case "esc":
			m.mode = pickList
			return m, nil
		case "enter":
			addr := strings.TrimSpace(m.input.Value())
			if addr == "" {
				return m, nil
			}
			if !strings.Contains(addr, ":") {
				addr += ":8080"
			}
			m.pendName, m.pendAddr, m.pendPrivate, m.pendHost = addr, addr, false, false
			return m, m.startPass("password (leave empty for public)")
		}

	case pickPass:
		switch key.String() {
		case "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		case "esc":
			m.mode = pickList
			return m, nil
		case "enter":
			pass := m.input.Value()
			if m.pendHost {
				m.finishHost(m.pendName, pass)
			} else {
				m.finish(m.pendAddr, m.pendName, pass != "" || m.pendPrivate, pass)
			}
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m pickerModel) View() string {
	var b strings.Builder
	b.WriteString("\n  " + titleSt.Render("chokuto") + dimSt.Render("  ·  connect or host a room") + "\n")
	b.WriteString("  " + dimSt.Render(strings.Repeat("─", 48)) + "\n\n")

	switch m.mode {
	case pickList:
		for i, e := range m.entries {
			cursor := "  "
			if i == m.cursor {
				cursor = fbCursorSt.Render("▶ ")
			}
			var line string
			switch {
			case e.create:
				name := dimSt.Render(e.name)
				if i == m.cursor {
					name = lipgloss.NewStyle().Bold(true).Foreground(clrGreen).Render(e.name)
				}
				line = cursor + onlineDotSt.Render("➕ ") + name
			case e.manual:
				name := dimSt.Render(e.name)
				if i == m.cursor {
					name = lipgloss.NewStyle().Foreground(clrAccent).Render(e.name)
				}
				line = cursor + "✎ " + name
			default:
				badge := onlineDotSt.Render("● public ")
				if e.private {
					badge = lipgloss.NewStyle().Foreground(clrOrange).Render("🔒 private")
				}
				name := lipgloss.NewStyle().Bold(true).Foreground(userColor(e.name)).Render(e.name)
				addr := dimSt.Render(e.addr)
				off := ""
				if !e.online {
					off = dimSt.Render("  (recent)")
				}
				line = cursor + name + "  " + badge + "  " + addr + off
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("\n  " + dimSt.Render("↑/↓ move   enter select   q quit"))

	case pickCreateName:
		b.WriteString("  " + dimSt.Render("New room name:") + "\n\n")
		b.WriteString("  " + m.input.View() + "\n")
		b.WriteString("\n  " + dimSt.Render("enter continue   esc back"))

	case pickAddr:
		b.WriteString("  " + dimSt.Render("Server address:") + "\n\n")
		b.WriteString("  " + m.input.View() + "\n")
		b.WriteString("\n  " + dimSt.Render("enter continue   esc back"))

	case pickPass:
		label := "Password for "
		if m.pendHost {
			label = "Set a password for "
		}
		b.WriteString("  " + dimSt.Render(label) + titleSt.Render(m.pendName) + dimSt.Render(":") + "\n")
		b.WriteString("  " + dimSt.Render("(leave empty for a public room)") + "\n\n")
		b.WriteString("  " + m.input.View() + "\n")
		b.WriteString("\n  " + dimSt.Render("enter "+map[bool]string{true: "create", false: "connect"}[m.pendHost]+"   esc back"))
	}

	return b.String()
}
