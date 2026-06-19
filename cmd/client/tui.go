package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"lan-drop/internal/protocol"
	"lan-drop/internal/transport"
)

const sidebarW = 24

// chunkSize is the file body chunk size; small enough to give smooth progress,
// large enough to keep framing overhead low.
const chunkSize = 32 * 1024

// ── palette ───────────────────────────────────────────────────────────────────

var (
	clrAccent = lipgloss.Color("75")
	clrGreen  = lipgloss.Color("82")
	clrOrange = lipgloss.Color("214")
	clrRed    = lipgloss.Color("203")
	clrPurple = lipgloss.Color("141")
	clrText   = lipgloss.Color("252")
	clrDim    = lipgloss.Color("244")
	clrFaint  = lipgloss.Color("240")
)

var (
	activeTabSt   = lipgloss.NewStyle().Bold(true).Foreground(clrAccent).Underline(true).Padding(0, 1)
	inactiveTabSt = lipgloss.NewStyle().Foreground(clrFaint).Padding(0, 1)
	titleSt       = lipgloss.NewStyle().Bold(true).Foreground(clrAccent)
	dimSt         = lipgloss.NewStyle().Foreground(clrFaint)
	joinSt        = lipgloss.NewStyle().Foreground(clrGreen).Italic(true)
	leaveSt       = lipgloss.NewStyle().Foreground(clrRed).Italic(true)
	meSt          = lipgloss.NewStyle().Bold(true).Foreground(clrAccent)
	fileSt        = lipgloss.NewStyle().Foreground(clrOrange)
	errSt         = lipgloss.NewStyle().Foreground(clrRed)
	onlineDotSt   = lipgloss.NewStyle().Foreground(clrGreen)
	typingSt      = lipgloss.NewStyle().Foreground(clrFaint).Italic(true)
	sideHeadSt    = lipgloss.NewStyle().Bold(true).Foreground(clrText)
	youTagSt      = lipgloss.NewStyle().Foreground(clrFaint)
	dmSt          = lipgloss.NewStyle().Bold(true).Foreground(clrPurple)

	fbPathSt   = lipgloss.NewStyle().Bold(true).Foreground(clrDim)
	fbDirSt    = lipgloss.NewStyle().Bold(true).Foreground(clrAccent)
	fbFileSt   = lipgloss.NewStyle().Foreground(clrText)
	fbSizeSt   = lipgloss.NewStyle().Foreground(clrFaint)
	fbCursorSt = lipgloss.NewStyle().Bold(true).Foreground(clrOrange)
)

var userPalette = []string{"75", "213", "82", "220", "205", "87", "214", "51"}

func userColor(name string) lipgloss.Color {
	var h int
	for _, c := range name {
		h = h*31 + int(c)
	}
	if h < 0 {
		h = -h
	}
	return lipgloss.Color(userPalette[h%len(userPalette)])
}

func styledName(name string) string {
	return lipgloss.NewStyle().Bold(true).Foreground(userColor(name)).Render(name)
}

func truncate(s string, maxW int) string {
	if lipgloss.Width(s) <= maxW {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes)+"…") > maxW {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

// ── types ─────────────────────────────────────────────────────────────────────

type tabIdx int

const (
	chatTab tabIdx = iota
	filesTab
)

type chatLine struct {
	kind string
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

type netEvent struct {
	line         chatLine
	users        []string
	usersUpdated bool
	typing       string
	progress     *transferMsg
}

// transferMsg is a progress update for one file transfer, in either direction.
type transferMsg struct {
	id       string
	dir      string // "up" or "down"
	name     string
	from     string
	done     int64
	total    int64
	finished bool
	err      error
}

type transferState struct {
	dir   string
	name  string
	from  string
	done  int64
	total int64
	start time.Time
}

type errMsg struct{ err error }

type tickMsg time.Time

// ── fileBrowser ───────────────────────────────────────────────────────────────

type fileBrowser struct {
	dir        string
	entries    []os.DirEntry
	cursor     int
	offset     int
	height     int
	width      int
	showHidden bool
}

func newFileBrowser() fileBrowser {
	homeDir, _ := os.UserHomeDir()
	dir := homeDir
	for _, cand := range []string{
		filepath.Join(homeDir, "Documents"),
		filepath.Join(homeDir, "Desktop"),
	} {
		if _, err := os.Stat(cand); err == nil {
			dir = cand
			break
		}
	}
	fb := fileBrowser{dir: dir}
	fb.reload()
	return fb
}

func (fb *fileBrowser) reload() {
	all, err := os.ReadDir(fb.dir)
	if err != nil {
		fb.entries = nil
		return
	}
	var dirs, files []os.DirEntry
	for _, e := range all {
		if !fb.showHidden && strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if e.IsDir() {
			dirs = append(dirs, e)
		} else {
			files = append(files, e)
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name() < dirs[j].Name() })
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })
	fb.entries = append(dirs, files...)
	if fb.cursor >= len(fb.entries) && len(fb.entries) > 0 {
		fb.cursor = len(fb.entries) - 1
	}
}

func (fb *fileBrowser) listH() int {
	h := fb.height - 2
	if h < 1 {
		h = 1
	}
	return h
}

func (fb *fileBrowser) moveUp() {
	if fb.cursor > 0 {
		fb.cursor--
		if fb.cursor < fb.offset {
			fb.offset = fb.cursor
		}
	}
}

func (fb *fileBrowser) moveDown() {
	if fb.cursor < len(fb.entries)-1 {
		fb.cursor++
		lh := fb.listH()
		if fb.cursor >= fb.offset+lh {
			fb.offset = fb.cursor - lh + 1
		}
	}
}

func (fb *fileBrowser) goUp() {
	parent := filepath.Dir(fb.dir)
	if parent == fb.dir {
		return
	}
	prev := filepath.Base(fb.dir)
	fb.dir = parent
	fb.cursor = 0
	fb.offset = 0
	fb.reload()
	for i, e := range fb.entries {
		if e.Name() == prev {
			fb.cursor = i
			lh := fb.listH()
			if fb.cursor >= lh {
				fb.offset = fb.cursor - lh/2
				if fb.offset < 0 {
					fb.offset = 0
				}
			}
			break
		}
	}
}

// enter navigates into a directory or returns the selected file path.
func (fb *fileBrowser) enter() (string, bool) {
	if len(fb.entries) == 0 || fb.cursor >= len(fb.entries) {
		return "", false
	}
	e := fb.entries[fb.cursor]
	path := filepath.Join(fb.dir, e.Name())
	if e.IsDir() {
		fb.dir = path
		fb.cursor = 0
		fb.offset = 0
		fb.reload()
		return "", false
	}
	return path, true
}

func (fb *fileBrowser) toggleHidden() {
	fb.showHidden = !fb.showHidden
	fb.reload()
}

func (fb fileBrowser) view() string {
	homeDir, _ := os.UserHomeDir()
	display := fb.dir
	if rel, err := filepath.Rel(homeDir, fb.dir); err == nil && !strings.HasPrefix(rel, "..") {
		if rel == "." {
			display = "~"
		} else {
			display = "~/" + rel
		}
	}

	extra := ""
	if fb.showHidden {
		extra = dimSt.Render("  · hidden")
	}

	var sb strings.Builder
	sb.WriteString(" " + fbPathSt.Render(display) + extra + "\n")
	sepW := fb.width - 2
	if sepW < 1 {
		sepW = 1
	}
	sb.WriteString(" " + dimSt.Render(strings.Repeat("─", sepW)) + "\n")

	lh := fb.listH()
	end := fb.offset + lh
	if end > len(fb.entries) {
		end = len(fb.entries)
	}
	visible := fb.entries[fb.offset:end]
	rendered := 0

	// Layout: " ▶ ▸ <name...>          9.9 MB"
	//          1  1 1 1 1               9
	const overhead = 5 + 9 // margin+cursor+sp+icon+sp | size
	nameMaxW := fb.width - overhead
	if nameMaxW < 4 {
		nameMaxW = 4
	}

	for i, e := range visible {
		idx := fb.offset + i
		selected := idx == fb.cursor

		cur := " "
		if selected {
			cur = fbCursorSt.Render("▶")
		}

		rawName := e.Name()
		var icon, nameStr, sizeStr string

		if e.IsDir() {
			rawName += "/"
			icon = fbDirSt.Render("▸")
			rawName = truncate(rawName, nameMaxW)
			pad := nameMaxW - lipgloss.Width(rawName)
			if pad < 0 {
				pad = 0
			}
			nameStr = fbDirSt.Render(rawName) + strings.Repeat(" ", pad)
			sizeStr = strings.Repeat(" ", 9)
		} else {
			icon = " "
			info, _ := e.Info()
			size := ""
			if info != nil {
				size = fmtSize(info.Size())
			}
			rawName = truncate(rawName, nameMaxW)
			pad := nameMaxW - lipgloss.Width(rawName)
			if pad < 0 {
				pad = 0
			}
			ns := fbFileSt
			if selected {
				ns = fbFileSt.Bold(true)
			}
			nameStr = ns.Render(rawName) + strings.Repeat(" ", pad)
			sizeStr = fbSizeSt.Render(fmt.Sprintf("%9s", size))
		}

		sb.WriteString(" " + cur + " " + icon + " " + nameStr + sizeStr + "\n")
		rendered++
	}

	if len(fb.entries) == 0 {
		sb.WriteString("   " + dimSt.Render("(empty directory)") + "\n")
		rendered++
	}

	for rendered < lh {
		sb.WriteString("\n")
		rendered++
	}

	return strings.TrimRight(sb.String(), "\n")
}

// ── model ─────────────────────────────────────────────────────────────────────

type model struct {
	tab            tabIdx
	lines          []chatLine
	files          []fileEntry
	vp             viewport.Model
	fb             fileBrowser
	input          textinput.Model
	prog           progress.Model
	conn           *transport.Conn
	username       string
	server         string
	histID         string // history file key (room name, stable across reconnects)
	private        bool
	eventCh        chan netEvent
	progressCh     chan transferMsg
	transfers      map[string]*transferState
	width          int
	height         int
	ready          bool
	users          []string
	typingUsers    map[string]time.Time
	lastTypingSent time.Time
	typingFrame    int
	showHelp       bool
	dmTarget       string // pinned DM recipient ("" = broadcast)
	userSelect     bool   // sidebar user-selection mode is active
	userCursor     int
}

// transferPanelH is the height reserved for the transfers panel in the Files tab.
const transferPanelH = 6

// browserH returns the file browser height, leaving room for the transfers panel.
func browserH(vpH int) int {
	h := vpH - transferPanelH
	if h < 3 {
		h = 3
	}
	return h
}

func newModel(conn *transport.Conn, username, server, histID string, private bool, history []chatLine, eventCh chan netEvent, progressCh chan transferMsg) model {
	ti := textinput.New()
	ti.Placeholder = "message...  (/dm <user> msg · drop a file path to send)"
	ti.Focus()
	ti.CharLimit = 1000

	const defaultW, defaultH = 80, 24
	vpH := defaultH - 6
	fb := newFileBrowser()
	fb.height = browserH(vpH)
	fb.width = defaultW
	vp := viewport.New(defaultW, vpH)

	lines := history
	if len(lines) > 0 {
		lines = append(lines, chatLine{kind: "sep", body: "previous messages", ts: time.Now()})
	}

	pr := progress.New(progress.WithDefaultGradient(), progress.WithoutPercentage())
	pr.Width = 20

	m := model{
		conn:        conn,
		username:    username,
		server:      server,
		histID:      histID,
		private:     private,
		eventCh:     eventCh,
		progressCh:  progressCh,
		transfers:   make(map[string]*transferState),
		input:       ti,
		fb:          fb,
		vp:          vp,
		prog:        pr,
		lines:       lines,
		width:       defaultW,
		height:      defaultH,
		ready:       true,
		typingUsers: make(map[string]time.Time),
	}
	m.vp.SetContent(m.chatContent())
	m.vp.GotoBottom()
	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, waitNet(m.eventCh), waitProgress(m.progressCh), doTick())
}

func waitNet(ch <-chan netEvent) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func waitProgress(ch <-chan transferMsg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func doTick() tea.Cmd {
	return tea.Tick(400*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// ── update ────────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		vpH := m.height - 6
		if vpH < 1 {
			vpH = 1
		}
		vpW := m.width
		if m.hasSidebar() {
			vpW = m.width - sidebarW - 1
		}
		m.vp = viewport.New(vpW, vpH)
		m.vp.SetContent(m.chatContent())
		m.vp.GotoBottom()
		m.fb.height = browserH(vpH)
		m.fb.width = m.width
		m.ready = true

	case tickMsg:
		m.typingFrame = (m.typingFrame + 1) % 3
		now := time.Now()
		for name, t := range m.typingUsers {
			if now.Sub(t) >= 3*time.Second {
				delete(m.typingUsers, name)
			}
		}
		cmds = append(cmds, doTick())

	case tea.KeyMsg:
		key := msg.String()

		// Help overlay swallows all keys; any key (besides quit) closes it.
		if m.showHelp {
			if key == "ctrl+c" || key == "ctrl+q" {
				return m, tea.Quit
			}
			m.showHelp = false
			return m, nil
		}

		// Sidebar user-selection mode: pick someone to DM.
		if m.userSelect {
			switch key {
			case "ctrl+c", "ctrl+q":
				return m, tea.Quit
			case "up", "k":
				if m.userCursor > 0 {
					m.userCursor--
				}
			case "down", "j":
				if m.userCursor < len(m.users)-1 {
					m.userCursor++
				}
			case "enter":
				if m.userCursor < len(m.users) {
					if t := m.users[m.userCursor]; t != m.username {
						m.dmTarget = t
					}
				}
				m.userSelect = false
			case "esc", "ctrl+u":
				m.userSelect = false
			}
			return m, nil
		}

		switch key {
		case "ctrl+c", "ctrl+q":
			return m, tea.Quit

		case "f1":
			m.showHelp = true
			return m, nil

		case "tab":
			m.tab = tabIdx(1 - int(m.tab))
			if m.tab == chatTab {
				m.input.Focus()
			} else {
				m.input.Blur()
			}

		case "ctrl+u":
			if m.tab == chatTab && len(m.users) > 0 {
				m.userSelect = true
				m.userCursor = 0
			}

		case "esc":
			if m.tab == chatTab && m.dmTarget != "" {
				m.dmTarget = ""
			}

		case "up", "k":
			if m.tab == filesTab {
				m.fb.moveUp()
			}

		case "down", "j":
			if m.tab == filesTab {
				m.fb.moveDown()
			}

		case "enter":
			if m.tab == filesTab {
				if path, isFile := m.fb.enter(); isFile {
					cmds = append(cmds, m.doSendFile(path, ""))
				}
			} else {
				cmds = append(cmds, m.handleChatEnter())
			}

		case "backspace", "left":
			if m.tab == filesTab {
				m.fb.goUp()
			}

		case ".":
			if m.tab == filesTab {
				m.fb.toggleHidden()
			}

		case "?":
			// In chat, only open help when the input is empty so '?' can still be
			// typed into a message.
			if m.tab == filesTab || m.input.Value() == "" {
				m.showHelp = true
				return m, nil
			}
		}

		// debounced typing indicator — check the key itself, not input.Value()
		// (input is updated later via m.input.Update, so Value() hasn't changed yet here)
		if m.tab == chatTab && (len([]rune(key)) == 1 || key == "backspace") {
			if time.Since(m.lastTypingSent) > time.Second {
				m.lastTypingSent = time.Now()
				m.conn.WriteFrame([]byte(protocol.Message{
					Type: protocol.TypeTyping,
					From: m.username,
				}.Encode()))
			}
		}

	case netEvent:
		if msg.usersUpdated {
			m.users = msg.users
		}
		if msg.typing != "" {
			m.typingUsers[msg.typing] = time.Now()
		}
		if msg.progress != nil {
			m.applyTransfer(*msg.progress)
		}
		if msg.line.kind != "" {
			m.record(msg.line)
		}
		cmds = append(cmds, waitNet(m.eventCh))

	case transferMsg:
		m.applyTransfer(msg)
		cmds = append(cmds, waitProgress(m.progressCh))

	case errMsg:
		m.record(chatLine{kind: "err", body: msg.err.Error(), ts: time.Now()})
	}

	if m.tab == chatTab {
		var vpCmd, inputCmd tea.Cmd
		m.vp, vpCmd = m.vp.Update(msg)
		m.input, inputCmd = m.input.Update(msg)
		cmds = append(cmds, vpCmd, inputCmd)
	}

	return m, tea.Batch(cmds...)
}

// handleChatEnter interprets the chat input as a DM command, a file path to
// send (drag & drop), or a normal broadcast message.
func (m *model) handleChatEnter() tea.Cmd {
	val := strings.TrimSpace(m.input.Value())
	m.input.Reset()
	if val == "" {
		return nil
	}

	// DM: /dm <user> <message>  (alias /w)
	if strings.HasPrefix(val, "/dm ") || strings.HasPrefix(val, "/w ") {
		rest := strings.TrimSpace(val[strings.IndexByte(val, ' ')+1:])
		parts := strings.SplitN(rest, " ", 2)
		if len(parts) == 2 && parts[0] != "" && strings.TrimSpace(parts[1]) != "" {
			to, body := parts[0], parts[1]
			m.conn.WriteFrame([]byte(protocol.Message{
				Type: protocol.TypeMessage, From: m.username, To: to, Body: body,
			}.Encode()))
			m.record(chatLine{kind: "dmout", from: to, body: body, ts: time.Now()})
		} else {
			m.record(chatLine{kind: "err", body: "usage: /dm <user> <message>", ts: time.Now()})
		}
		return nil
	}

	// Drag & drop: an existing file path becomes a file send.
	if p := cleanDropPath(val); p != "" {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return m.doSendFile(p, "")
		}
	}

	// A pinned DM target sends privately until cleared with esc.
	if m.dmTarget != "" {
		m.conn.WriteFrame([]byte(protocol.Message{
			Type: protocol.TypeMessage, From: m.username, To: m.dmTarget, Body: val,
		}.Encode()))
		m.record(chatLine{kind: "dmout", from: m.dmTarget, body: val, ts: time.Now()})
		return nil
	}

	// Normal broadcast message.
	m.conn.WriteFrame([]byte(protocol.Message{
		Type: protocol.TypeMessage, From: m.username, Body: val,
	}.Encode()))
	m.record(chatLine{kind: "me", from: m.username, body: val, ts: time.Now()})
	return nil
}

// cleanDropPath normalises a path that a terminal pasted on file drop (quotes,
// escaped spaces, ~). Returns "" if the input clearly isn't a path.
func cleanDropPath(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			s = s[1 : len(s)-1]
		}
	}
	s = strings.ReplaceAll(s, "\\ ", " ")
	if strings.HasPrefix(s, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			s = filepath.Join(home, s[2:])
		}
	}
	if !strings.ContainsAny(s, "/\\") {
		return "" // a bare word is a message, not a path
	}
	return s
}

// applyTransfer updates progress state and, on completion, records a chat line.
func (m *model) applyTransfer(t transferMsg) {
	if t.err != nil {
		delete(m.transfers, t.id)
		m.record(chatLine{kind: "err", body: "transfer failed: " + t.err.Error(), ts: time.Now()})
		return
	}
	if t.finished {
		delete(m.transfers, t.id)
		if t.dir == "down" {
			m.files = append(m.files, fileEntry{filename: t.name, size: t.total, from: t.from, ts: time.Now()})
			m.record(chatLine{kind: "file", from: t.from, body: fmt.Sprintf("sent %s (%s)", t.name, fmtSize(t.total)), ts: time.Now()})
		} else {
			m.record(chatLine{kind: "file", from: m.username, body: fmt.Sprintf("sent %s (%s)", t.name, fmtSize(t.total)), ts: time.Now()})
			m.tab = chatTab
			m.input.Focus()
		}
		return
	}
	st := m.transfers[t.id]
	if st == nil {
		st = &transferState{dir: t.dir, name: t.name, from: t.from, total: t.total, start: time.Now()}
		m.transfers[t.id] = st
	}
	st.done = t.done
}

// ── view ──────────────────────────────────────────────────────────────────────

func (m model) hasSidebar() bool {
	return m.width >= 72
}

func (m model) View() string {
	if !m.ready {
		return "  connecting..."
	}
	if m.showHelp {
		return m.viewHelp()
	}
	return strings.Join([]string{
		m.viewHeader(),
		m.viewContent(),
		m.viewStatusLine(),
		m.viewInput(),
		m.viewStatus(),
	}, "\n")
}

func (m model) viewHelp() string {
	row := func(keys, desc string) string {
		return "  " + lipgloss.NewStyle().Bold(true).Foreground(clrAccent).Width(16).Render(keys) + dimSt.Render(desc)
	}
	lines := []string{
		"",
		"  " + titleSt.Render("chokuto") + dimSt.Render("  ·  help"),
		"  " + dimSt.Render(strings.Repeat("─", 44)),
		"",
		row("enter", "send message / send selected file"),
		row("/dm <user> msg", "send a private message (alias /w)"),
		row("ctrl+u", "pick a user from the sidebar to DM"),
		row("esc", "leave DM mode (back to everyone)"),
		row("drop a file", "drag a file in, then enter, to send it"),
		row("tab", "switch between Chat and Files"),
		row("↑/↓ · pgup/dn", "navigate files / scroll chat"),
		row(".", "toggle hidden files (Files tab)"),
		row("? · f1", "toggle this help"),
		row("ctrl+q", "quit"),
		"",
		"  " + dimSt.Render("press any key to close"),
	}
	out := strings.Join(lines, "\n")
	// pad to roughly fill the screen so the overlay feels intentional
	for n := strings.Count(out, "\n"); n < m.height-1; n++ {
		out += "\n"
	}
	return out
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

	left := titleSt.Render("chokuto") + dimSt.Render("  ·  ") + chatLabel + dimSt.Render("·") + filesLabel

	right := ""
	if m.private {
		right += lipgloss.NewStyle().Foreground(clrOrange).Render("🔒 ")
	}
	if n := len(m.users); n > 0 {
		right += onlineDotSt.Render("●") + dimSt.Render(fmt.Sprintf(" %d online  ", n))
	}

	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	gap := m.width - lw - rw
	if gap < 0 {
		gap = 0
	}
	line := left + strings.Repeat(" ", gap) + right
	border := dimSt.Render(strings.Repeat("─", m.width))
	return line + "\n" + border
}

func (m model) viewContent() string {
	if m.tab == filesTab {
		return strings.Join(m.transfersPanel(), "\n") + "\n" + m.fb.view()
	}
	if !m.hasSidebar() {
		return m.vp.View()
	}
	return m.viewChatWithSidebar()
}

// transfersPanel renders the Files-tab header: active transfers (with progress
// bars) followed by recently received files. Always exactly transferPanelH lines.
func (m model) transfersPanel() []string {
	sepW := m.width - 2
	if sepW < 1 {
		sepW = 1
	}
	lines := []string{
		" " + sideHeadSt.Render("Transfers") + dimSt.Render("   received files save to where you launched the client"),
		" " + dimSt.Render(strings.Repeat("─", sepW)),
	}

	var rows []string
	for _, id := range sortedKeys(m.transfers) {
		if len(rows) >= transferPanelH-2 {
			break
		}
		st := m.transfers[id]
		frac := 0.0
		if st.total > 0 {
			frac = float64(st.done) / float64(st.total)
		}
		arrow := "⬆"
		if st.dir == "down" {
			arrow = "⬇"
		}
		rows = append(rows, " "+fileSt.Render(arrow+" "+truncate(st.name, 16))+" "+m.prog.ViewAs(frac)+
			dimSt.Render(fmt.Sprintf(" %3.0f%%", frac*100)))
	}
	for i := len(m.files) - 1; i >= 0 && len(rows) < transferPanelH-2; i-- {
		f := m.files[i]
		rows = append(rows, " "+fileSt.Render("✓ ")+truncate(f.filename, 22)+
			dimSt.Render(fmt.Sprintf("   %s  from %s", fmtSize(f.size), f.from)))
	}
	if len(rows) == 0 {
		rows = append(rows, "   "+dimSt.Render("(no transfers yet — send a file from Chat, or pick one below)"))
	}

	lines = append(lines, rows...)
	for len(lines) < transferPanelH {
		lines = append(lines, "")
	}
	return lines[:transferPanelH]
}

func sortedKeys(m map[string]*transferState) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (m model) viewChatWithSidebar() string {
	chatWidth := m.width - sidebarW - 1
	vpLines := strings.Split(m.vp.View(), "\n")
	sideLines := m.buildSidebarLines()

	var sb strings.Builder
	for i := 0; i < m.vp.Height; i++ {
		cl := ""
		if i < len(vpLines) {
			cl = vpLines[i]
		}
		vis := lipgloss.Width(cl)
		if vis < chatWidth {
			cl += strings.Repeat(" ", chatWidth-vis)
		}
		sl := ""
		if i < len(sideLines) {
			sl = sideLines[i]
		}
		sb.WriteString(cl)
		sb.WriteString(dimSt.Render("│"))
		sb.WriteString(sl)
		if i < m.vp.Height-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func (m model) buildSidebarLines() []string {
	lines := make([]string, 0, m.vp.Height)
	head := fmt.Sprintf(" Online (%d)", len(m.users))
	if m.userSelect {
		head = " Pick a user…"
	}
	lines = append(lines, sideHeadSt.Render(head))
	lines = append(lines, dimSt.Render(" "+strings.Repeat("─", sidebarW-2)))

	now := time.Now()
	for i, u := range m.users {
		marker := " "
		if m.userSelect && i == m.userCursor {
			marker = fbCursorSt.Render("▶")
		} else if !m.userSelect && u == m.dmTarget {
			marker = dmSt.Render("◆")
		}
		dot := onlineDotSt.Render("●")
		name := lipgloss.NewStyle().Bold(true).Foreground(userColor(u)).Render(u)
		you := ""
		if u == m.username {
			you = youTagSt.Render(" (you)")
		}
		typing := ""
		if t, ok := m.typingUsers[u]; ok && now.Sub(t) < 3*time.Second {
			dots := []string{"·", "··", "···"}[m.typingFrame]
			typing = typingSt.Render(" " + dots)
		}
		lines = append(lines, marker+dot+" "+name+you+typing)
	}

	for len(lines) < m.vp.Height {
		lines = append(lines, "")
	}
	return lines
}

// viewStatusLine shows an active transfer's progress bar if any, otherwise the
// typing indicator. Always exactly one line so the layout stays stable.
func (m model) viewStatusLine() string {
	if m.tab != chatTab {
		return ""
	}
	if line := m.viewTransferLine(); line != "" {
		return line
	}
	return m.viewTypingLine()
}

func (m model) viewTransferLine() string {
	if len(m.transfers) == 0 {
		return ""
	}
	// Pick a deterministic transfer to display (first by id).
	st := m.transfers[sortedKeys(m.transfers)[0]]

	frac := 0.0
	if st.total > 0 {
		frac = float64(st.done) / float64(st.total)
	}
	arrow := "⬆"
	if st.dir == "down" {
		arrow = "⬇"
	}
	name := truncate(st.name, 18)
	pct := fmt.Sprintf(" %3.0f%%  %s/%s", frac*100, fmtSize(st.done), fmtSize(st.total))
	extra := ""
	if len(m.transfers) > 1 {
		extra = dimSt.Render(fmt.Sprintf("  (+%d)", len(m.transfers)-1))
	}
	return " " + fileSt.Render(arrow+" "+name) + " " + m.prog.ViewAs(frac) + dimSt.Render(pct) + extra
}

// viewTypingLine always returns exactly one line so the layout stays stable.
func (m model) viewTypingLine() string {
	var who []string
	now := time.Now()
	for name, t := range m.typingUsers {
		if now.Sub(t) < 3*time.Second && name != m.username {
			who = append(who, name)
		}
	}
	if len(who) == 0 {
		return " " // reserve the line without visible content
	}

	sort.Strings(who)
	dots := []string{"·", "··", "···"}[m.typingFrame]

	var text string
	switch len(who) {
	case 1:
		text = styledName(who[0]) + typingSt.Render(" is typing "+dots)
	case 2:
		text = styledName(who[0]) + typingSt.Render(", ") + styledName(who[1]) + typingSt.Render(" are typing "+dots)
	default:
		text = typingSt.Render(fmt.Sprintf("%d people are typing %s", len(who), dots))
	}
	return " " + text
}

func (m model) viewInput() string {
	border := dimSt.Render(strings.Repeat("─", m.width))
	if m.tab == filesTab {
		hint := dimSt.Render("  ↑/↓ navigate   enter open/send   ⌫ go up   . hidden   ? help")
		return border + "\n" + hint
	}
	if m.userSelect {
		return border + "\n" + dimSt.Render("  ↑/↓ pick a user   enter start DM   esc cancel")
	}
	prompt := dimSt.Render(" › ")
	if m.dmTarget != "" {
		prompt = dmSt.Render(" [DM → " + m.dmTarget + "] ")
	}
	return border + "\n" + prompt + m.input.View()
}

func (m model) viewStatus() string {
	userPart := lipgloss.NewStyle().Bold(true).Foreground(userColor(m.username)).Render(m.username)
	left := " " + userPart + dimSt.Render("@"+m.server)
	hint := "ctrl+u dm   tab switch   ? help   ctrl+q quit "
	switch {
	case m.tab == filesTab:
		hint = "tab switch   ? help   ctrl+q quit "
	case m.dmTarget != "":
		hint = "esc leave dm   tab switch   ? help   ctrl+q quit "
	}
	right := dimSt.Render(hint)
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	return left + strings.Repeat(" ", gap) + right
}

// ── helpers ───────────────────────────────────────────────────────────────────

// record appends a line, persists it to history (if persistent), and scrolls.
func (m *model) record(l chatLine) {
	m.lines = append(m.lines, l)
	appendHistory(m.histID, l)
	m.refreshVP()
}

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
	case "sep":
		return dimSt.Render("  ── " + l.body + " ──")
	case "join":
		return ts + " " + joinSt.Render("⊕  "+l.body)
	case "leave":
		return ts + " " + leaveSt.Render("⊖  "+l.body)
	case "me":
		return ts + " " + meSt.Render(l.from) + sep + l.body
	case "msg":
		name := lipgloss.NewStyle().Bold(true).Foreground(userColor(l.from)).Render(l.from)
		return ts + " " + name + sep + l.body
	case "dm":
		return ts + " " + dmSt.Render("[DM] ") + styledName(l.from) + sep + l.body
	case "dmout":
		return ts + " " + dmSt.Render("[DM → "+l.from+"]") + sep + l.body
	case "file":
		return ts + " " + fileSt.Render("⬇  "+l.from+": "+l.body)
	case "err":
		return ts + " " + errSt.Render("✗  "+l.body)
	default:
		return ts + " " + dimSt.Render(l.body)
	}
}

func (m model) doSendFile(path, to string) tea.Cmd {
	conn, username, ch := m.conn, m.username, m.progressCh
	return func() tea.Msg {
		go streamFile(conn, username, to, path, ch)
		return nil
	}
}

// streamFile sends a file as a header frame followed by chunk frames, reporting
// progress on ch.
func streamFile(conn *transport.Conn, username, to, path string, ch chan<- transferMsg) {
	f, err := os.Open(path)
	if err != nil {
		ch <- transferMsg{err: err}
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		ch <- transferMsg{err: err}
		return
	}
	size := info.Size()
	name := filepath.Base(path)
	id := fmt.Sprintf("%s-%d", username, time.Now().UnixNano())

	hdr := protocol.FileHeader{From: username, To: to, ID: id, Size: size, Filename: name}
	if err := conn.WriteFrame([]byte(hdr.Encode())); err != nil {
		ch <- transferMsg{id: id, err: err}
		return
	}
	ch <- transferMsg{id: id, dir: "up", name: name, total: size}

	buf := make([]byte, chunkSize)
	var sent int64
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			if err := conn.WriteFrame(protocol.EncodeChunk(id, buf[:n])); err != nil {
				ch <- transferMsg{id: id, err: err}
				return
			}
			sent += int64(n)
			ch <- transferMsg{id: id, dir: "up", name: name, done: sent, total: size}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			ch <- transferMsg{id: id, err: rerr}
			return
		}
	}
	ch <- transferMsg{id: id, dir: "up", name: name, done: size, total: size, finished: true}
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

type recvState struct {
	name  string
	from  string
	size  int64
	recvd int64
	f     *os.File
}

func startNetworkReader(conn *transport.Conn, username string, eventCh chan<- netEvent) {
	recv := make(map[string]*recvState)

	for {
		frame, err := conn.ReadFrame()
		if err != nil {
			eventCh <- netEvent{line: chatLine{kind: "err", body: "disconnected from server", ts: time.Now()}}
			return
		}

		// File body chunk.
		if id, data, ok := protocol.DecodeChunk(frame); ok {
			st := recv[id]
			if st == nil {
				continue
			}
			if _, err := st.f.Write(data); err != nil {
				st.f.Close()
				delete(recv, id)
				eventCh <- netEvent{line: chatLine{kind: "err", body: "file write failed: " + err.Error(), ts: time.Now()}}
				continue
			}
			st.recvd += int64(len(data))
			eventCh <- netEvent{progress: &transferMsg{id: id, dir: "down", name: st.name, from: st.from, done: st.recvd, total: st.size}}
			if st.recvd >= st.size {
				st.f.Close()
				delete(recv, id)
				eventCh <- netEvent{progress: &transferMsg{id: id, dir: "down", name: st.name, from: st.from, done: st.size, total: st.size, finished: true}}
			}
			continue
		}

		s := string(frame)

		// File header: open the destination file for the chunks that follow.
		if strings.HasPrefix(s, "FILE:") {
			fh, err := protocol.DecodeFileHeader(s)
			if err != nil {
				continue
			}
			name := filepath.Base(fh.Filename)
			f, err := os.Create(name)
			if err != nil {
				eventCh <- netEvent{line: chatLine{kind: "err", body: "file save failed: " + err.Error(), ts: time.Now()}}
				continue
			}
			st := &recvState{name: name, from: fh.From, size: fh.Size, f: f}
			recv[fh.ID] = st
			eventCh <- netEvent{progress: &transferMsg{id: fh.ID, dir: "down", name: name, from: fh.From, done: 0, total: fh.Size}}
			if fh.Size == 0 {
				f.Close()
				delete(recv, fh.ID)
				eventCh <- netEvent{progress: &transferMsg{id: fh.ID, dir: "down", name: name, from: fh.From, finished: true}}
			}
			continue
		}

		msg, err := protocol.Decode(s)
		if err != nil {
			continue
		}

		switch msg.Type {
		case protocol.TypeJoin:
			eventCh <- netEvent{line: chatLine{kind: "join", from: msg.From, body: msg.Body, ts: time.Now()}}
		case protocol.TypeLeave:
			eventCh <- netEvent{line: chatLine{kind: "leave", from: msg.From, body: msg.Body, ts: time.Now()}}
		case protocol.TypeMessage:
			if msg.From == username {
				continue
			}
			kind := "msg"
			if msg.To == username {
				kind = "dm"
			}
			eventCh <- netEvent{line: chatLine{kind: kind, from: msg.From, body: msg.Body, ts: time.Now()}}
		case protocol.TypeTyping:
			if msg.From != username {
				eventCh <- netEvent{typing: msg.From}
			}
		case protocol.TypeUserList:
			body := strings.TrimSpace(msg.Body)
			var users []string
			if body != "" {
				users = strings.Split(body, ",")
			}
			eventCh <- netEvent{users: users, usersUpdated: true}
		}
	}
}
