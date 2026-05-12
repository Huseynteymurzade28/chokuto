package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"lan-drop/internal/protocol"
)

const sidebarW = 24

// ── palette ───────────────────────────────────────────────────────────────────

var (
	clrAccent = lipgloss.Color("75")
	clrGreen  = lipgloss.Color("82")
	clrOrange = lipgloss.Color("214")
	clrRed    = lipgloss.Color("203")
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
	file         *fileEntry
	users        []string
	usersUpdated bool
	typing       string
}

type fileSentMsg struct {
	name string
	size int64
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
	conn           net.Conn
	username       string
	server         string
	eventCh        chan netEvent
	width          int
	height         int
	ready          bool
	users          []string
	typingUsers    map[string]time.Time
	lastTypingSent time.Time
	typingFrame    int
}

func newModel(conn net.Conn, username, server string, eventCh chan netEvent) model {
	ti := textinput.New()
	ti.Placeholder = "message..."
	ti.Focus()
	ti.CharLimit = 1000

	return model{
		conn:        conn,
		username:    username,
		server:      server,
		eventCh:     eventCh,
		input:       ti,
		fb:          newFileBrowser(),
		typingUsers: make(map[string]time.Time),
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, waitNet(m.eventCh), doTick())
}

func waitNet(ch <-chan netEvent) tea.Cmd {
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
		m.fb.height = vpH
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
		switch msg.String() {
		case "ctrl+c", "ctrl+q":
			return m, tea.Quit

		case "tab":
			m.tab = tabIdx(1 - int(m.tab))
			if m.tab == chatTab {
				m.input.Placeholder = "message..."
				m.input.Focus()
			} else {
				m.input.Reset()
				m.input.Blur()
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
					cmds = append(cmds, m.doSendFile(path))
				}
			} else {
				val := strings.TrimSpace(m.input.Value())
				m.input.Reset()
				if val != "" {
					fmt.Fprint(m.conn, protocol.Message{
						Type: protocol.TypeMessage,
						From: m.username,
						Body: val,
					}.Encode())
					m.lines = append(m.lines, chatLine{
						kind: "me", from: m.username, body: val, ts: time.Now(),
					})
					m.refreshVP()
				}
			}

		case "backspace", "left":
			if m.tab == filesTab {
				m.fb.goUp()
			}

		case ".":
			if m.tab == filesTab {
				m.fb.toggleHidden()
			}
		}

		// debounced typing indicator — check the key itself, not input.Value()
		// (input is updated later via m.input.Update, so Value() hasn't changed yet here)
		k := msg.String()
		if m.tab == chatTab && (len([]rune(k)) == 1 || k == "backspace") {
			if time.Since(m.lastTypingSent) > time.Second {
				m.lastTypingSent = time.Now()
				fmt.Fprint(m.conn, protocol.Message{
					Type: protocol.TypeTyping,
					From: m.username,
					Body: "",
				}.Encode())
			}
		}

	case netEvent:
		if msg.usersUpdated {
			m.users = msg.users
		}
		if msg.typing != "" {
			m.typingUsers[msg.typing] = time.Now()
		}
		if msg.file != nil {
			m.files = append(m.files, *msg.file)
			m.lines = append(m.lines, chatLine{
				kind: "file",
				from: msg.file.from,
				body: fmt.Sprintf("sent %s (%s)", msg.file.filename, fmtSize(msg.file.size)),
				ts:   msg.file.ts,
			})
			m.refreshVP()
		} else if msg.line.kind != "" {
			m.lines = append(m.lines, msg.line)
			m.refreshVP()
		}
		cmds = append(cmds, waitNet(m.eventCh))

	case fileSentMsg:
		m.lines = append(m.lines, chatLine{
			kind: "file",
			from: m.username,
			body: fmt.Sprintf("sent %s (%s)", msg.name, fmtSize(msg.size)),
			ts:   time.Now(),
		})
		m.tab = chatTab
		m.input.Placeholder = "message..."
		m.input.Focus()
		m.refreshVP()

	case errMsg:
		m.lines = append(m.lines, chatLine{kind: "err", body: msg.err.Error(), ts: time.Now()})
		m.refreshVP()
	}

	if m.tab == chatTab {
		var vpCmd, inputCmd tea.Cmd
		m.vp, vpCmd = m.vp.Update(msg)
		m.input, inputCmd = m.input.Update(msg)
		cmds = append(cmds, vpCmd, inputCmd)
	}

	return m, tea.Batch(cmds...)
}

// ── view ──────────────────────────────────────────────────────────────────────

func (m model) hasSidebar() bool {
	return m.width >= 72
}

func (m model) View() string {
	if !m.ready {
		return "  connecting..."
	}
	return strings.Join([]string{
		m.viewHeader(),
		m.viewContent(),
		m.viewTypingLine(),
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

	left := titleSt.Render("lan-drop") + dimSt.Render("  ·  ") + chatLabel + dimSt.Render("·") + filesLabel

	right := ""
	if n := len(m.users); n > 0 {
		right = onlineDotSt.Render("●") + dimSt.Render(fmt.Sprintf(" %d online  ", n))
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
		return m.fb.view()
	}
	if !m.hasSidebar() {
		return m.vp.View()
	}
	return m.viewChatWithSidebar()
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
	lines = append(lines, sideHeadSt.Render(fmt.Sprintf(" Online (%d)", len(m.users))))
	lines = append(lines, dimSt.Render(" "+strings.Repeat("─", sidebarW-2)))

	now := time.Now()
	for _, u := range m.users {
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
		lines = append(lines, " "+dot+" "+name+you+typing)
	}

	for len(lines) < m.vp.Height {
		lines = append(lines, "")
	}
	return lines
}

// viewTypingLine always returns exactly one line so the layout stays stable.
func (m model) viewTypingLine() string {
	if m.tab != chatTab {
		return ""
	}
	var who []string
	now := time.Now()
	for name, t := range m.typingUsers {
		if now.Sub(t) < 3*time.Second {
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
		hint := dimSt.Render("  ↑/↓  navigate    enter  open / send file    ⌫  go up    .  toggle hidden")
		return border + "\n" + hint
	}
	return border + "\n" + dimSt.Render(" › ") + m.input.View()
}

func (m model) viewStatus() string {
	userPart := lipgloss.NewStyle().Bold(true).Foreground(userColor(m.username)).Render(m.username)
	left := " " + userPart + dimSt.Render("@"+m.server)
	hint := "tab: switch  pgup/dn: scroll  ctrl+q: quit "
	if m.tab == filesTab {
		hint = "tab: switch  ctrl+q: quit "
	}
	right := dimSt.Render(hint)
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
		return ts + " " + joinSt.Render("⊕  "+l.body)
	case "leave":
		return ts + " " + leaveSt.Render("⊖  "+l.body)
	case "me":
		return ts + " " + meSt.Render(l.from) + sep + l.body
	case "msg":
		name := lipgloss.NewStyle().Bold(true).Foreground(userColor(l.from)).Render(l.from)
		return ts + " " + name + sep + l.body
	case "file":
		return ts + " " + fileSt.Render("⬇  "+l.from+": "+l.body)
	case "err":
		return ts + " " + errSt.Render("✗  "+l.body)
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

		switch msg.Type {
		case protocol.TypeJoin:
			eventCh <- netEvent{line: chatLine{kind: "join", from: msg.From, body: msg.Body, ts: time.Now()}}
		case protocol.TypeLeave:
			eventCh <- netEvent{line: chatLine{kind: "leave", from: msg.From, body: msg.Body, ts: time.Now()}}
		case protocol.TypeMessage:
			if msg.From == username {
				continue
			}
			eventCh <- netEvent{line: chatLine{kind: "msg", from: msg.From, body: msg.Body, ts: time.Now()}}
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
